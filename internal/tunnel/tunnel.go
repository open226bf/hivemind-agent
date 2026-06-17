// Package tunnel establishes the reverse tunnel to the Hivemind server: the
// agent dials out, upgrades the connection, then accepts multiplexed streams and
// proxies each one to the local Docker daemon. From the server's side this looks
// like a direct Docker API connection — no inbound exposure on the cluster.
package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/yamux"
)

const proto = "hivemind-tunnel"

// Serve opens the tunnel and serves Docker proxy streams until ctx is cancelled
// or the connection drops. It returns the error that ended the session so the
// caller can reconnect.
func Serve(ctx context.Context, serverURL, agentID, dockerAddr string, insecure bool) error {
	conn, err := dialUpgrade(ctx, serverURL, agentID, insecure)
	if err != nil {
		return err
	}
	defer conn.Close()

	// The agent accepts streams the server opens for each Docker API call.
	session, err := yamux.Server(conn, nil)
	if err != nil {
		return fmt.Errorf("yamux server: %w", err)
	}
	defer session.Close()

	go func() {
		<-ctx.Done()
		_ = session.Close()
	}()

	for {
		stream, err := session.Accept()
		if err != nil {
			return fmt.Errorf("tunnel closed: %w", err)
		}
		go proxyToDocker(stream, dockerAddr)
	}
}

// dialUpgrade connects to the server and performs the HTTP upgrade handshake,
// returning the raw connection ready for yamux.
func dialUpgrade(ctx context.Context, serverURL, agentID string, insecure bool) (net.Conn, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("server url: %w", err)
	}
	host := u.Host
	if u.Port() == "" {
		if u.Scheme == "https" {
			host += ":443"
		} else {
			host += ":80"
		}
	}

	d := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	if u.Scheme == "https" {
		conn, err = tls.DialWithDialer(d, "tcp", host, &tls.Config{InsecureSkipVerify: insecure}) //nolint:gosec // opt-in dev flag
	} else {
		conn, err = d.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, fmt.Errorf("dial server: %w", err)
	}

	req := fmt.Sprintf(
		"GET /api/v1/agent/connect?agent_id=%s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\n\r\n",
		url.QueryEscape(agentID), u.Host, proto,
	)
	if _, err := io.WriteString(conn, req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("send upgrade: %w", err)
	}

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read upgrade response: %w", err)
	}
	if !strings.Contains(status, "101") {
		_ = conn.Close()
		return nil, fmt.Errorf("server refused tunnel: %s", strings.TrimSpace(status))
	}
	// Drain the remaining response headers.
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("read upgrade headers: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	// Hand yamux a conn that first drains anything the bufio reader buffered.
	return &bufConn{Conn: conn, r: br}, nil
}

// proxyToDocker pipes a tunnel stream to the local Docker daemon, bidirectionally.
func proxyToDocker(stream net.Conn, dockerAddr string) {
	defer stream.Close()
	network, address := dockerNetwork(dockerAddr)
	dc, err := net.DialTimeout(network, address, 10*time.Second)
	if err != nil {
		return
	}
	defer dc.Close()

	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(dc, stream); done <- struct{}{} }()
	go func() { _, _ = io.Copy(stream, dc); done <- struct{}{} }()
	<-done
}

// dockerNetwork resolves the Docker endpoint to a (network, address) pair,
// defaulting to the local unix socket.
func dockerNetwork(dockerAddr string) (network, address string) {
	switch {
	case dockerAddr == "":
		return "unix", "/var/run/docker.sock"
	case strings.HasPrefix(dockerAddr, "unix://"):
		return "unix", strings.TrimPrefix(dockerAddr, "unix://")
	case strings.HasPrefix(dockerAddr, "tcp://"):
		return "tcp", strings.TrimPrefix(dockerAddr, "tcp://")
	default:
		return "unix", dockerAddr
	}
}

// bufConn is a net.Conn whose reads first drain a buffered reader (the leftover
// bytes from the HTTP upgrade) before falling through to the raw connection.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

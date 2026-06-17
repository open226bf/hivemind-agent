// Package tunnel establishes the reverse tunnel to the Hivemind server: the
// agent dials out, upgrades the connection, then accepts multiplexed streams and
// proxies each one to the local Docker daemon. From the server's side this looks
// like a direct Docker API connection — no inbound exposure on the cluster.
//
// Two transports are supported: a plain/HTTPS connection authenticated by agent
// id (dev/token mode) and a mutual-TLS connection authenticated by the agent's
// client certificate (production).
package tunnel

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/hashicorp/yamux"
)

const proto = "hivemind-tunnel"

// NodeQuery is the node identity advertised on the connect URL.
type NodeQuery struct {
	NodeID        string
	Hostname      string
	Role          string
	IsManager     bool
	IsLeader      bool
	EngineVersion string
}

func (n NodeQuery) values() url.Values {
	v := url.Values{}
	v.Set("node_id", n.NodeID)
	v.Set("hostname", n.Hostname)
	v.Set("role", n.Role)
	v.Set("engine_version", n.EngineVersion)
	if n.IsManager {
		v.Set("is_manager", "true")
	}
	if n.IsLeader {
		v.Set("is_leader", "true")
	}
	return v
}

// Serve opens a token-mode tunnel (agent id auth) and serves Docker proxy
// streams until ctx is cancelled or the connection drops.
func Serve(ctx context.Context, serverURL, agentID, dockerAddr string, insecure bool) error {
	u, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("server url: %w", err)
	}
	host := hostPort(u.Host, u.Scheme == "https")

	conn, err := dial(ctx, u.Scheme == "https", host, nil, insecure)
	if err != nil {
		return err
	}
	path := "/api/v1/agent/connect?agent_id=" + url.QueryEscape(agentID)
	ready, err := upgrade(conn, u.Host, path)
	if err != nil {
		_ = conn.Close()
		return err
	}
	return serveSession(ctx, ready, dockerAddr)
}

// ServeMTLS opens a mutual-TLS tunnel authenticated by the agent's client
// certificate. hubAddr is host:port; node identity is advertised on the URL.
func ServeMTLS(ctx context.Context, hubAddr string, clientCert, clientKey, caCert []byte, node NodeQuery, dockerAddr string, insecure bool) error {
	cert, err := tls.X509KeyPair(clientCert, clientKey)
	if err != nil {
		return fmt.Errorf("client key pair: %w", err)
	}
	pool := x509.NewCertPool()
	if len(caCert) > 0 && !pool.AppendCertsFromPEM(caCert) {
		return fmt.Errorf("invalid CA certificate")
	}
	host, _, _ := net.SplitHostPort(hubAddr)
	cfg := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            pool,
		ServerName:         host,
		InsecureSkipVerify: insecure, //nolint:gosec // opt-in dev flag
		NextProtos:         []string{"http/1.1"},
		MinVersion:         tls.VersionTLS12,
	}
	conn, err := dial(ctx, true, hubAddr, cfg, insecure)
	if err != nil {
		return err
	}
	path := "/api/v1/agent/connect?" + node.values().Encode()
	ready, err := upgrade(conn, hubAddr, path)
	if err != nil {
		_ = conn.Close()
		return err
	}
	return serveSession(ctx, ready, dockerAddr)
}

// dial opens a raw TCP or TLS connection. When tlsCfg is non-nil it is used as-is.
func dial(ctx context.Context, useTLS bool, host string, tlsCfg *tls.Config, insecure bool) (net.Conn, error) {
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !useTLS {
		conn, err := d.DialContext(ctx, "tcp", host)
		if err != nil {
			return nil, fmt.Errorf("dial server: %w", err)
		}
		return conn, nil
	}
	if tlsCfg == nil {
		tlsCfg = &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec // opt-in dev flag
	}
	conn, err := tls.DialWithDialer(d, "tcp", host, tlsCfg)
	if err != nil {
		return nil, fmt.Errorf("dial server (tls): %w", err)
	}
	return conn, nil
}

// upgrade sends the HTTP upgrade request and consumes the 101 response, returning
// a conn whose reads include any bytes the response buffer already held.
func upgrade(conn net.Conn, hostHeader, path string) (net.Conn, error) {
	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: %s\r\n\r\n",
		path, hostHeader, proto,
	)
	if _, err := io.WriteString(conn, req); err != nil {
		return nil, fmt.Errorf("send upgrade: %w", err)
	}
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read upgrade response: %w", err)
	}
	if !strings.Contains(status, "101") {
		return nil, fmt.Errorf("server refused tunnel: %s", strings.TrimSpace(status))
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read upgrade headers: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return &bufConn{Conn: conn, r: br}, nil
}

// serveSession runs the yamux server and proxies each accepted stream to Docker.
func serveSession(ctx context.Context, conn net.Conn, dockerAddr string) error {
	defer conn.Close()
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

func hostPort(host string, https bool) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	if https {
		return host + ":443"
	}
	return host + ":80"
}

// bufConn is a net.Conn whose reads first drain a buffered reader (the leftover
// bytes from the HTTP upgrade) before falling through to the raw connection.
type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufConn) Read(p []byte) (int, error) { return c.r.Read(p) }

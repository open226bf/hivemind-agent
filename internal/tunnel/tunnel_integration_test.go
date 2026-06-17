package tunnel

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/yamux"
)

// TestTunnelProxiesToRealDocker exercises the data plane against the local Docker
// daemon: a yamux stream (what the server opens) is proxied by the agent to
// docker.sock, and a real Docker /_ping returns 200. Skipped when no local
// docker socket is available.
func TestTunnelProxiesToRealDocker(t *testing.T) {
	const sock = "/var/run/docker.sock"
	if c, err := net.DialTimeout("unix", sock, time.Second); err != nil {
		t.Skipf("no local docker socket (%v)", err)
	} else {
		_ = c.Close()
	}

	// Pipe stands in for the post-upgrade tunnel connection.
	hubSide, agentSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Agent side: serve the session, proxying each stream to docker.sock.
	go func() { _ = serveSession(ctx, agentSide, sock) }()

	// Hub side: open a stream and speak the Docker API over it.
	sess, err := yamux.Client(hubSide, nil)
	if err != nil {
		t.Fatalf("yamux client: %v", err)
	}
	defer sess.Close()

	stream, err := sess.Open()
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	if _, err := stream.Write([]byte("GET /_ping HTTP/1.1\r\nHost: docker\r\nConnection: close\r\n\r\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	_ = stream.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("read docker response over tunnel: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("docker /_ping over tunnel: status %d, want 200", resp.StatusCode)
	}
	t.Logf("docker reachable over tunnel: %s api=%s", resp.Status, strings.TrimSpace(resp.Header.Get("Api-Version")))
}

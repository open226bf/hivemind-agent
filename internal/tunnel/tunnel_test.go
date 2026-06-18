package tunnel

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestDockerNetwork(t *testing.T) {
	cases := []struct {
		in, wantNet, wantAddr string
	}{
		{"", "unix", "/var/run/docker.sock"},
		{"unix:///var/run/docker.sock", "unix", "/var/run/docker.sock"},
		{"tcp://127.0.0.1:2375", "tcp", "127.0.0.1:2375"},
		{"/custom/docker.sock", "unix", "/custom/docker.sock"},
	}
	for _, c := range cases {
		gotNet, gotAddr := dockerNetwork(c.in)
		if gotNet != c.wantNet || gotAddr != c.wantAddr {
			t.Errorf("dockerNetwork(%q) = (%q,%q), want (%q,%q)", c.in, gotNet, gotAddr, c.wantNet, c.wantAddr)
		}
	}
}

func TestHostPort(t *testing.T) {
	cases := []struct {
		host  string
		https bool
		want  string
	}{
		{"example.com", false, "example.com:80"},
		{"example.com", true, "example.com:443"},
		{"example.com:9000", true, "example.com:9000"},
	}
	for _, c := range cases {
		if got := hostPort(c.host, c.https); got != c.want {
			t.Errorf("hostPort(%q,%v) = %q, want %q", c.host, c.https, got, c.want)
		}
	}
}

// TestUpgrade_SendsHeadersAndConsumes101 verifies that upgrade emits the extra
// headers (the enrollment token), accepts a 101 response, and exposes the bytes
// that arrived after the response headers (the start of the tunnel stream).
func TestUpgrade_SendsHeadersAndConsumes101(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	var gotRequest string
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		br := bufio.NewReader(server)
		var sb strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			sb.WriteString(line)
			if strings.TrimSpace(line) == "" {
				break
			}
		}
		gotRequest = sb.String()
		// Respond 101 and leak a sentinel byte that belongs to the tunnel stream.
		_, _ = io.WriteString(server, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: "+proto+"\r\n\r\nZ")
	}()

	conn, err := upgrade(client, "hub.local", "/api/v1/agent/connect?agent_id=a1",
		map[string]string{tokenHeader: "secret-token"})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}

	<-serverDone
	if !strings.Contains(gotRequest, tokenHeader+": secret-token\r\n") {
		t.Errorf("request missing token header:\n%s", gotRequest)
	}
	if !strings.Contains(gotRequest, "Upgrade: "+proto+"\r\n") {
		t.Errorf("request missing upgrade header:\n%s", gotRequest)
	}

	// The post-handshake byte must be readable from the returned conn.
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 1)
	if _, err := io.ReadFull(conn, b); err != nil || b[0] != 'Z' {
		t.Errorf("leftover read = %q (err %v), want 'Z'", b, err)
	}
}

// TestUpgrade_RejectsNon101 verifies a non-101 status is surfaced as an error.
func TestUpgrade_RejectsNon101(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		br := bufio.NewReader(server)
		for {
			line, err := br.ReadString('\n')
			if err != nil || strings.TrimSpace(line) == "" {
				break
			}
		}
		_, _ = io.WriteString(server, "HTTP/1.1 401 Unauthorized\r\n\r\n")
	}()

	if _, err := upgrade(client, "hub.local", "/x", nil); err == nil {
		t.Fatal("expected an error for a non-101 response")
	}
}

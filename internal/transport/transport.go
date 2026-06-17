// Package transport defines the agent↔server contract and an HTTP client for it.
//
// This is the bootstrap transport: the agent dials out to the server to enroll
// and to send heartbeats. The full multiplexed reverse tunnel (Docker API proxy
// + per-node channel) is layered on top of this connection in a later phase.
package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// NodeInfo is the node identity the agent reports to the server.
type NodeInfo struct {
	NodeID        string `json:"node_id"`
	Hostname      string `json:"hostname"`
	Role          string `json:"role"`
	IsManager     bool   `json:"is_manager"`
	IsLeader      bool   `json:"is_leader"`
	EngineVersion string `json:"engine_version"`
	SwarmID       string `json:"swarm_id"`
}

// RegisterRequest enrolls (first connection) or re-identifies an agent.
type RegisterRequest struct {
	EnrollToken string   `json:"enroll_token,omitempty"`
	AgentID     string   `json:"agent_id,omitempty"`
	Node        NodeInfo `json:"node"`
}

// RegisterResponse returns the assigned agent identity and the cluster it is
// bound to.
type RegisterResponse struct {
	AgentID     string `json:"agent_id"`
	ClusterID   string `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
}

// HeartbeatRequest reports liveness and the current node role (which can change
// on leader re-election / promotion).
type HeartbeatRequest struct {
	AgentID string   `json:"agent_id"`
	Node    NodeInfo `json:"node"`
}

// Server is the agent's view of the Hivemind server.
type Server interface {
	Register(ctx context.Context, req RegisterRequest) (RegisterResponse, error)
	Heartbeat(ctx context.Context, req HeartbeatRequest) error
}

// HTTPServer talks to the Hivemind agent-hub HTTP endpoints.
type HTTPServer struct {
	base string
	http *http.Client
}

// NewHTTPServer builds an HTTP server client. insecure disables TLS verification
// (development only).
func NewHTTPServer(baseURL string, insecure bool) *HTTPServer {
	return &HTTPServer{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}}, //nolint:gosec // opt-in dev flag
		},
	}
}

func (s *HTTPServer) Register(ctx context.Context, req RegisterRequest) (RegisterResponse, error) {
	var resp RegisterResponse
	if err := s.post(ctx, "/api/v1/agent/register", req, &resp); err != nil {
		return RegisterResponse{}, err
	}
	return resp, nil
}

func (s *HTTPServer) Heartbeat(ctx context.Context, req HeartbeatRequest) error {
	return s.post(ctx, "/api/v1/agent/heartbeat", req, nil)
}

func (s *HTTPServer) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := s.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("server %s: %s: %s", path, res.Status, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

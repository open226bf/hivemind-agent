// Package agent runs the agent lifecycle: enroll with the server, then keep a
// liveness loop. The reverse-tunnel data plane (Docker proxy + node channel)
// will attach to the same connection in a later phase.
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/orange/hivemind-agent/internal/config"
	"github.com/orange/hivemind-agent/internal/identity"
	"github.com/orange/hivemind-agent/internal/transport"
	"github.com/orange/hivemind-agent/internal/tunnel"
)

const (
	minBackoff = 1 * time.Second
	maxBackoff = 30 * time.Second
)

// Run enrolls the agent and maintains heartbeats until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, ident identity.NodeIdentity, srv transport.Server) error {
	node := toNodeInfo(ident)

	// mTLS mode: the client certificate is the credential — no token handshake.
	if cfg.CertMode() {
		slog.Info("agent starting (mTLS)",
			"node_id", node.NodeID, "role", node.Role, "leader", node.IsLeader, "hub", cfg.HubAddr)
		serveMTLSTunnel(ctx, cfg, ident)
		return nil
	}

	slog.Info("agent starting",
		"node_id", node.NodeID, "role", node.Role, "leader", node.IsLeader,
		"swarm_id", node.SwarmID, "server", cfg.ServerURL)

	agentID, err := register(ctx, cfg, node, srv)
	if err != nil {
		return err // ctx cancelled during registration
	}

	// Reverse tunnel (data plane): reconnects with capped backoff until ctx ends.
	go serveTunnel(ctx, cfg, agentID, toNodeQuery(ident))

	ticker := time.NewTicker(cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("agent stopping")
			return nil
		case <-ticker.C:
			if err := srv.Heartbeat(ctx, transport.HeartbeatRequest{AgentID: agentID, Node: node}); err != nil {
				slog.Warn("heartbeat failed", "err", err)
			}
		}
	}
}

// register retries enrollment with capped backoff until it succeeds or ctx ends.
func register(ctx context.Context, cfg config.Config, node transport.NodeInfo, srv transport.Server) (string, error) {
	backoff := minBackoff
	for {
		resp, err := srv.Register(ctx, transport.RegisterRequest{
			EnrollToken: cfg.EnrollToken,
			AgentID:     cfg.AgentID,
			Node:        node,
		})
		if err == nil {
			slog.Info("agent enrolled", "agent_id", resp.AgentID, "cluster", resp.ClusterName)
			return resp.AgentID, nil
		}
		slog.Warn("registration failed, retrying", "err", err, "in", backoff)

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// serveTunnel keeps the token-mode reverse tunnel up, reconnecting with backoff.
// The enrollment token authenticates the tunnel (the agent id alone is public);
// the node identity is advertised on the URL so the hub keys this tunnel by node
// (a global agent has one tunnel per node — without it they collide on an empty
// id and evict each other).
func serveTunnel(ctx context.Context, cfg config.Config, agentID string, node tunnel.NodeQuery) {
	reconnect(ctx, func() error {
		return tunnel.Serve(ctx, cfg.ServerURL, agentID, cfg.EnrollToken, cfg.DockerHost, node, cfg.InsecureSkipVerify)
	})
}

// serveMTLSTunnel keeps the mutual-TLS tunnel up, reconnecting with backoff.
func serveMTLSTunnel(ctx context.Context, cfg config.Config, ident identity.NodeIdentity) {
	reconnect(ctx, func() error {
		return tunnel.ServeMTLS(ctx, cfg.HubAddr,
			[]byte(cfg.ClientCert), []byte(cfg.ClientKey), []byte(cfg.CACert),
			toNodeQuery(ident), cfg.DockerHost, cfg.InsecureSkipVerify)
	})
}

// reconnect runs fn, retrying with capped backoff until ctx is cancelled.
func reconnect(ctx context.Context, fn func() error) {
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return
		}
		err := fn()
		if ctx.Err() != nil {
			return
		}
		slog.Warn("tunnel ended, reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// toNodeQuery projects the detected node identity into the tunnel's URL query
// form, shared by the token and mutual-TLS transports.
func toNodeQuery(i identity.NodeIdentity) tunnel.NodeQuery {
	return tunnel.NodeQuery{
		NodeID:        i.NodeID,
		Hostname:      i.Hostname,
		Role:          i.Role,
		IsManager:     i.IsManager,
		IsLeader:      i.IsLeader,
		EngineVersion: i.EngineVersion,
	}
}

func toNodeInfo(i identity.NodeIdentity) transport.NodeInfo {
	return transport.NodeInfo{
		NodeID:        i.NodeID,
		Hostname:      i.Hostname,
		Role:          i.Role,
		IsManager:     i.IsManager,
		IsLeader:      i.IsLeader,
		EngineVersion: i.EngineVersion,
		SwarmID:       i.SwarmID,
	}
}

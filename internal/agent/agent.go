// Package agent runs the agent lifecycle: enroll with the server, then keep a
// liveness loop. The reverse-tunnel data plane (Docker proxy + node channel)
// will attach to the same connection in a later phase.
package agent

import (
	"context"
	"log/slog"
	"time"

	"github.com/open226bf/hivemind-agent/internal/config"
	"github.com/open226bf/hivemind-agent/internal/identity"
	"github.com/open226bf/hivemind-agent/internal/transport"
)

const (
	minBackoff = 1 * time.Second
	maxBackoff = 30 * time.Second
)

// Run enrolls the agent and maintains heartbeats until ctx is cancelled.
func Run(ctx context.Context, cfg config.Config, ident identity.NodeIdentity, srv transport.Server) error {
	node := toNodeInfo(ident)
	slog.Info("agent starting",
		"node_id", node.NodeID, "role", node.Role, "leader", node.IsLeader,
		"swarm_id", node.SwarmID, "server", cfg.ServerURL)

	agentID, err := register(ctx, cfg, node, srv)
	if err != nil {
		return err // ctx cancelled during registration
	}

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

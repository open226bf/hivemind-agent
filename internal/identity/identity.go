// Package identity detects the node the agent runs on via the local Docker
// daemon, so the server can route control-plane traffic to a manager and
// node-scoped traffic to the right node.
package identity

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

// NodeIdentity describes the Swarm node the agent task is running on.
type NodeIdentity struct {
	NodeID        string
	Hostname      string
	Role          string // "manager" | "worker"
	IsManager     bool
	IsLeader      bool
	EngineVersion string
	SwarmID       string // Swarm cluster id — lets the server de-duplicate clusters
}

// Detect inspects the local Docker daemon to build the node identity. dockerHost
// may be empty to use the ambient environment.
func Detect(ctx context.Context, dockerHost string) (NodeIdentity, error) {
	opts := []client.Opt{client.FromEnv, client.WithAPIVersionNegotiation()}
	if dockerHost != "" {
		opts = append(opts, client.WithHost(dockerHost))
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return NodeIdentity{}, fmt.Errorf("docker client: %w", err)
	}
	defer cli.Close()

	info, err := cli.Info(ctx)
	if err != nil {
		return NodeIdentity{}, fmt.Errorf("docker info: %w", err)
	}
	if info.Swarm.NodeID == "" || info.Swarm.LocalNodeState != swarm.LocalNodeStateActive {
		return NodeIdentity{}, fmt.Errorf("node is not part of an active swarm (state=%s)", info.Swarm.LocalNodeState)
	}

	id := NodeIdentity{
		NodeID:        info.Swarm.NodeID,
		Hostname:      info.Name,
		EngineVersion: info.ServerVersion,
		IsManager:     info.Swarm.ControlAvailable, // only managers expose the control API
	}
	// Swarm.Cluster carries the cluster id but is populated only on managers;
	// workers report a nil ClusterInfo, so guard before dereferencing it.
	if info.Swarm.Cluster != nil {
		id.SwarmID = info.Swarm.Cluster.ID
	}
	id.Role = "worker"
	if id.IsManager {
		id.Role = "manager"
		// Leadership is only knowable from a manager (it can inspect nodes).
		if node, _, err := cli.NodeInspectWithRaw(ctx, info.Swarm.NodeID); err == nil {
			id.IsLeader = node.ManagerStatus != nil && node.ManagerStatus.Leader
		}
	}
	return id, nil
}

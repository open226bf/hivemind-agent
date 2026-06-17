// Package config loads the Hivemind agent configuration from the environment.
package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config is the agent runtime configuration. It is intentionally small: the
// agent dials out to the Hivemind server, so it only needs where to connect and
// how to authenticate.
type Config struct {
	// ServerURL is the Hivemind agent-hub base URL the agent dials out to.
	ServerURL string
	// EnrollToken is the one-time enrollment token (first connection only).
	EnrollToken string
	// AgentID identifies an already-enrolled agent (set after enrollment so the
	// agent reconnects without a token). Empty on first boot.
	AgentID string
	// DockerHost overrides the Docker endpoint; empty uses the ambient default
	// (the node's local /var/run/docker.sock).
	DockerHost string
	// Heartbeat is the interval between liveness pings to the server.
	Heartbeat time.Duration
	// InsecureSkipVerify disables TLS verification toward the server (dev only).
	InsecureSkipVerify bool
}

// ErrMissingServer is returned when HIVEMIND_SERVER is not set.
var ErrMissingServer = errors.New("HIVEMIND_SERVER is required")

// Load reads the configuration from environment variables and validates it.
func Load() (Config, error) {
	cfg := Config{
		ServerURL:          os.Getenv("HIVEMIND_SERVER"),
		EnrollToken:        os.Getenv("HIVEMIND_ENROLL_TOKEN"),
		AgentID:            os.Getenv("HIVEMIND_AGENT_ID"),
		DockerHost:         os.Getenv("DOCKER_HOST"),
		Heartbeat:          15 * time.Second,
		InsecureSkipVerify: parseBool(os.Getenv("HIVEMIND_INSECURE_SKIP_VERIFY")),
	}
	if cfg.ServerURL == "" {
		return Config{}, ErrMissingServer
	}
	if v := os.Getenv("HIVEMIND_HEARTBEAT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Heartbeat = d
		}
	}
	return cfg, nil
}

func parseBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

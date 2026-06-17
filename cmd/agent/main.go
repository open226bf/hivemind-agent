// Command agent is the Hivemind agent: it runs on a cluster (global Swarm
// service, one task per node), dials out to the Hivemind server, enrolls and
// reports liveness. The reverse-tunnel data plane is layered on later.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/orange/hivemind-agent/internal/agent"
	"github.com/orange/hivemind-agent/internal/config"
	"github.com/orange/hivemind-agent/internal/identity"
	"github.com/orange/hivemind-agent/internal/transport"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ident, err := identity.Detect(ctx, cfg.DockerHost)
	if err != nil {
		slog.Error("node identity detection failed", "err", err)
		os.Exit(1)
	}

	srv := transport.NewHTTPServer(cfg.ServerURL, cfg.InsecureSkipVerify)
	if err := agent.Run(ctx, cfg, ident, srv); err != nil {
		slog.Error("agent terminated", "err", err)
		os.Exit(1)
	}
}

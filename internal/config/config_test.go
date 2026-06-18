package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/open226bf/hivemind-agent/internal/config"
)

func TestCertMode(t *testing.T) {
	full := config.Config{HubAddr: "hub:9443", ClientCert: "cert", ClientKey: "key"}
	if !full.CertMode() {
		t.Error("CertMode() = false, want true when hub + cert + key are all set")
	}
	for name, c := range map[string]config.Config{
		"no hub":  {ClientCert: "cert", ClientKey: "key"},
		"no cert": {HubAddr: "hub:9443", ClientKey: "key"},
		"no key":  {HubAddr: "hub:9443", ClientCert: "cert"},
		"empty":   {},
	} {
		if c.CertMode() {
			t.Errorf("CertMode() = true for %q, want false", name)
		}
	}
}

func TestLoad_TokenMode(t *testing.T) {
	t.Setenv("HIVEMIND_SERVER", "https://hivemind.example")
	t.Setenv("HIVEMIND_ENROLL_TOKEN", "tok-123")
	t.Setenv("HIVEMIND_AGENT_ID", "agent-1")
	t.Setenv("HIVEMIND_HEARTBEAT", "5s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ServerURL != "https://hivemind.example" || cfg.EnrollToken != "tok-123" || cfg.AgentID != "agent-1" {
		t.Errorf("unexpected config: %+v", cfg)
	}
	if cfg.Heartbeat != 5*time.Second {
		t.Errorf("Heartbeat = %v, want 5s", cfg.Heartbeat)
	}
	if cfg.CertMode() {
		t.Error("token-mode config should not report CertMode")
	}
}

func TestLoad_RequiresServerWithoutCert(t *testing.T) {
	t.Setenv("HIVEMIND_SERVER", "")
	t.Setenv("HIVEMIND_HUB_ADDR", "")
	t.Setenv("HIVEMIND_CLIENT_CERT", "")
	t.Setenv("HIVEMIND_CLIENT_KEY", "")
	if _, err := config.Load(); !errors.Is(err, config.ErrMissingServer) {
		t.Fatalf("Load err = %v, want ErrMissingServer", err)
	}
}

func TestLoad_CertModeNeedsNoServer(t *testing.T) {
	t.Setenv("HIVEMIND_SERVER", "")
	t.Setenv("HIVEMIND_HUB_ADDR", "hub:9443")
	t.Setenv("HIVEMIND_CLIENT_CERT", "cert-pem")
	t.Setenv("HIVEMIND_CLIENT_KEY", "key-pem")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load (cert mode): %v", err)
	}
	if !cfg.CertMode() {
		t.Error("expected CertMode in mTLS configuration")
	}
}

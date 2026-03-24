package cmd

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/wesm/msgvault/internal/config"
	"github.com/wesm/msgvault/internal/remote"
)

func TestResolveMCPEngine_Remote(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Remote.URL = srv.URL
	cfg.Remote.APIKey = "test-key"
	cfg.Remote.AllowInsecure = true

	result, err := resolveMCPEngine(cfg, true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = result.Cleanup() }()

	if !result.IsRemote {
		t.Fatal("expected IsRemote to be true")
	}
	if result.AttachmentsDir != "" {
		t.Errorf("expected empty AttachmentsDir, got %q", result.AttachmentsDir)
	}
	if result.DataDir != "" {
		t.Errorf("expected empty DataDir, got %q", result.DataDir)
	}
	if result.Engine == nil {
		t.Fatal("expected non-nil engine")
	}
	if _, ok := result.Engine.(*remote.Engine); !ok {
		t.Errorf("expected *remote.Engine, got %T", result.Engine)
	}
}

func TestResolveMCPEngine_RemoteError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Remote.URL = "http://example.com:8080"
	cfg.Remote.AllowInsecure = false

	_, err := resolveMCPEngine(cfg, true, false, false)
	if err == nil {
		t.Fatal("expected error for insecure URL without AllowInsecure")
	}
}

func TestResolveMCPEngine_RemoteInvalidURL(t *testing.T) {
	cfg := &config.Config{}
	cfg.Remote.URL = "://bad-url"
	cfg.Remote.AllowInsecure = true

	_, err := resolveMCPEngine(cfg, true, false, false)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestResolveMCPEngine_RemoteCleanup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Remote.URL = srv.URL
	cfg.Remote.AllowInsecure = true

	result, err := resolveMCPEngine(cfg, true, false, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := result.Cleanup(); err != nil {
		t.Fatalf("cleanup returned error: %v", err)
	}
}

func TestResolveMCPEngine_Local(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "msgvault.db")

	cfg := &config.Config{}
	cfg.Data.DataDir = tmpDir
	cfg.Data.DatabaseURL = dbPath

	result, err := resolveMCPEngine(cfg, false, true, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = result.Cleanup() }()

	if result.IsRemote {
		t.Fatal("expected IsRemote to be false")
	}
	if result.AttachmentsDir != filepath.Join(tmpDir, "attachments") {
		t.Errorf("unexpected AttachmentsDir: %q", result.AttachmentsDir)
	}
	if result.DataDir != tmpDir {
		t.Errorf("unexpected DataDir: %q", result.DataDir)
	}
	if result.Engine == nil {
		t.Fatal("expected non-nil engine")
	}
}

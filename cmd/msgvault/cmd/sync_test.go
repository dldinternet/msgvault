package cmd

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/wesm/msgvault/internal/config"
	"github.com/wesm/msgvault/internal/store"
)

// fakeClientSecrets is a minimal Google OAuth client_secret.json that
// oauth.NewManager can parse. No real credentials are exposed.
const fakeClientSecrets = `{
  "installed": {
    "client_id": "test.apps.googleusercontent.com",
    "client_secret": "test-secret",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "redirect_uris": ["http://localhost"]
  }
}`

// captureStdout runs fn while capturing os.Stdout output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	_ = r.Close()
	return string(out)
}

// TestSyncCmd_DuplicateIdentifierRoutesCorrectly verifies that when
// Gmail and IMAP sources share the same identifier, the single-arg
// sync path resolves both and routes each to the correct backend.
//
// Regression test: before the fix, GetSourceByIdentifier returned
// an arbitrary single row, so one source type would be lost.
// The Gmail source is seeded with a SyncCursor and valid OAuth
// scaffolding so the test exercises runIncrementalSync, not just
// the OAuth manager setup.
func TestSyncCmd_DuplicateIdentifierRoutesCorrectly(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/msgvault.db"

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.InitSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	// Create both gmail and imap sources for the same identifier.
	gmailSrc, err := s.GetOrCreateSource(
		"gmail", "shared@example.com",
	)
	if err != nil {
		t.Fatalf("create gmail source: %v", err)
	}
	// Set a history cursor so runIncrementalSync proceeds past
	// the "no history ID" guard and into getTokenSourceWithReauth.
	if err := s.UpdateSourceSyncCursor(gmailSrc.ID, "99999"); err != nil {
		t.Fatalf("set sync cursor: %v", err)
	}

	_, err = s.GetOrCreateSource("imap", "shared@example.com")
	if err != nil {
		t.Fatalf("create imap source: %v", err)
	}
	_ = s.Close()

	// Write a minimal client_secret.json so the OAuth manager
	// can be created without error.
	secretsPath := filepath.Join(tmpDir, "client_secret.json")
	err = os.WriteFile(
		secretsPath, []byte(fakeClientSecrets), 0600,
	)
	if err != nil {
		t.Fatalf("write client secrets: %v", err)
	}

	savedCfg := cfg
	savedLogger := logger
	defer func() {
		cfg = savedCfg
		logger = savedLogger
	}()

	cfg = &config.Config{
		HomeDir: tmpDir,
		Data:    config.DataConfig{DataDir: tmpDir},
		OAuth:   config.OAuthConfig{ClientSecrets: secretsPath},
	}
	logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

	testCmd := &cobra.Command{
		Use:  "sync [email]",
		Args: cobra.MaximumNArgs(1),
		RunE: syncIncrementalCmd.RunE,
	}

	root := newTestRootCmd()
	root.AddCommand(testCmd)
	root.SetArgs([]string{"sync", "shared@example.com"})

	// Capture stdout: the sync command prints per-source errors
	// to stdout while the returned error is just the count.
	var execErr error
	output := captureStdout(t, func() {
		execErr = root.Execute()
	})

	if execErr == nil {
		t.Fatal("expected error (no credentials/token)")
	}

	errMsg := execErr.Error()

	// Should NOT hit the legacy Gmail-only fallback, which sets
	// source to nil and produces "no source found".
	if strings.Contains(output, "no source found") {
		t.Error("should not hit legacy Gmail-only fallback path")
	}

	// Both sources should be resolved and attempted, producing
	// 2 failures (IMAP: missing config, Gmail: missing token).
	if !strings.Contains(errMsg, "2 account(s) failed") {
		t.Errorf(
			"expected both sources resolved; got: %s",
			errMsg,
		)
	}

	// The Gmail error should come from inside runIncrementalSync
	// (reaching getTokenSourceWithReauth), not from OAuth manager
	// creation. "add-account" appears only in the token-missing
	// error produced by getTokenSourceWithReauth.
	if !strings.Contains(output, "add-account") {
		t.Errorf(
			"Gmail error should originate from "+
				"runIncrementalSync; output:\n%s",
			output,
		)
	}
}

// TestSyncCmd_SingleSourceNoAmbiguity verifies that a single
// source for an identifier works without the legacy fallback.
func TestSyncCmd_SingleSourceNoAmbiguity(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/msgvault.db"

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := s.InitSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	_, err = s.GetOrCreateSource("imap", "solo@example.com")
	if err != nil {
		t.Fatalf("create imap source: %v", err)
	}
	_ = s.Close()

	savedCfg := cfg
	savedLogger := logger
	defer func() {
		cfg = savedCfg
		logger = savedLogger
	}()

	cfg = &config.Config{
		HomeDir: tmpDir,
		Data:    config.DataConfig{DataDir: tmpDir},
	}
	logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

	testCmd := &cobra.Command{
		Use:  "sync [email]",
		Args: cobra.MaximumNArgs(1),
		RunE: syncIncrementalCmd.RunE,
	}

	root := newTestRootCmd()
	root.AddCommand(testCmd)
	root.SetArgs([]string{"sync", "solo@example.com"})

	err = root.Execute()
	if err == nil {
		t.Fatal("expected error (no IMAP config)")
	}

	errMsg := err.Error()

	// Exactly 1 source should fail (IMAP with missing config).
	if !strings.Contains(errMsg, "1 account(s) failed") {
		t.Errorf(
			"expected 1 failed account; got: %s",
			errMsg,
		)
	}

	// Should NOT hit legacy fallback (source exists in DB).
	if strings.Contains(errMsg, "no source found") {
		t.Error("should not hit legacy fallback path")
	}
}

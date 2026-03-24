package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wesm/msgvault/internal/config"
	mcpserver "github.com/wesm/msgvault/internal/mcp"
	"github.com/wesm/msgvault/internal/query"
	"github.com/wesm/msgvault/internal/remote"
	"github.com/wesm/msgvault/internal/store"
)

var mcpForceSQL bool
var mcpNoSQLiteScanner bool

// mcpEngineResult holds the resolved engine and associated resource paths.
type mcpEngineResult struct {
	Engine         query.Engine
	AttachmentsDir string
	DataDir        string
	IsRemote       bool
	Cleanup        func() error
}

// resolveMCPEngine selects the appropriate query engine based on configuration.
// When isRemote is true, it creates a remote.Engine and sets AttachmentsDir
// and DataDir to empty strings.
// When local, it opens SQLite, initializes schema, starts FTS backfill,
// and selects DuckDB/Parquet or SQLite engine.
func resolveMCPEngine(
	cfg *config.Config,
	isRemote bool,
	forceSQL bool,
	noSQLiteScanner bool,
) (*mcpEngineResult, error) {
	if isRemote {
		remoteCfg := remote.Config{
			URL:           cfg.Remote.URL,
			APIKey:        cfg.Remote.APIKey,
			AllowInsecure: cfg.Remote.AllowInsecure,
		}
		remoteEngine, err := remote.NewEngine(remoteCfg)
		if err != nil {
			return nil, fmt.Errorf("connect to remote: %w", err)
		}
		return &mcpEngineResult{
			Engine:   remoteEngine,
			IsRemote: true,
			Cleanup:  remoteEngine.Close,
		}, nil
	}

	// Local mode — open SQLite and select query engine
	dbPath := cfg.DatabaseDSN()
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := s.InitSchema(); err != nil {
		s.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	// Build FTS index in background — MCP should start serving immediately
	if s.NeedsFTSBackfill() {
		go func() {
			_, _ = s.BackfillFTS(nil)
		}()
	}

	var engine query.Engine
	var duckEngine *query.DuckDBEngine
	analyticsDir := cfg.AnalyticsDir()

	if !forceSQL && query.HasCompleteParquetData(analyticsDir) {
		var duckOpts query.DuckDBOptions
		if noSQLiteScanner {
			duckOpts.DisableSQLiteScanner = true
		}
		de, err := query.NewDuckDBEngine(analyticsDir, dbPath, s.DB(), duckOpts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to open Parquet engine: %v\n", err)
			fmt.Fprintf(os.Stderr, "Falling back to SQLite\n")
			engine = query.NewSQLiteEngine(s.DB())
		} else {
			duckEngine = de
			engine = duckEngine
		}
	} else {
		engine = query.NewSQLiteEngine(s.DB())
	}

	cleanup := func() error {
		if duckEngine != nil {
			duckEngine.Close()
		}
		return s.Close()
	}

	return &mcpEngineResult{
		Engine:         engine,
		AttachmentsDir: cfg.AttachmentsDir(),
		DataDir:        cfg.Data.DataDir,
		Cleanup:        cleanup,
	}, nil
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Run MCP server for Claude Desktop integration",
	Long: `Start an MCP (Model Context Protocol) server over stdio.

This allows Claude Desktop (or any MCP client) to query your email archive
using tools like search_messages, get_message, list_messages, get_stats,
aggregate, and stage_deletion.

Add to Claude Desktop config:
  {
    "mcpServers": {
      "msgvault": {
        "command": "msgvault",
        "args": ["mcp"]
      }
    }
  }`,
	RunE: func(cmd *cobra.Command, args []string) error {
		result, err := resolveMCPEngine(cfg, IsRemoteMode(), mcpForceSQL, mcpNoSQLiteScanner)
		if err != nil {
			return err
		}
		defer func() {
			if err := result.Cleanup(); err != nil {
				fmt.Fprintf(os.Stderr, "msgvault MCP cleanup error: %v\n", err)
			}
		}()

		if result.IsRemote {
			fmt.Fprintf(os.Stderr, "msgvault MCP: connected to remote server %s\n", cfg.Remote.URL)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		return mcpserver.Serve(ctx, result.Engine, result.AttachmentsDir, result.DataDir)
	},
}

func init() {
	rootCmd.AddCommand(mcpCmd)
	mcpCmd.Flags().BoolVar(&mcpForceSQL, "force-sql", false, "Force SQLite queries instead of Parquet")
	mcpCmd.Flags().BoolVar(&mcpNoSQLiteScanner, "no-sqlite-scanner", false, "Disable DuckDB sqlite_scanner extension (use direct SQLite fallback)")
	_ = mcpCmd.Flags().MarkHidden("no-sqlite-scanner")
}

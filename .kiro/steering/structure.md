# Project Structure

## Organization Pattern

Standard Go project layout: `cmd/` for the CLI entrypoint, `internal/` for all library packages (enforces encapsulation), top-level config files.

```
msgvault/
  cmd/msgvault/          # Binary entrypoint
    main.go              # Signal handling, context setup
    cmd/                 # ~50 Cobra command files (one file per command)
  internal/              # 24 packages, all private to this module
  docs/                  # API documentation
  scripts/               # Development/benchmark tools
  Makefile               # Build targets
  go.mod / go.sum        # Dependencies
```

## CLI Command Pattern (`cmd/msgvault/cmd/`)

Each command is a single file named after the command (e.g., `syncfull.go`, `import_mbox.go`, `search.go`). Every file follows the same structure:

1. Define a `cobra.Command` variable
2. Register it via `init()` calling `rootCmd.AddCommand(...)`
3. Flags defined in `init()`
4. `root.go` handles config loading, logging, and global flags (`--config`, `--home`, `--verbose`, `--local`)

Shared CLI utilities live in `output.go` (formatting), `store_resolver.go` (database access), and `cliprogress_test.go` (progress bar helpers).

## Internal Package Responsibilities

| Package | Role |
|---------|------|
| `store` | SQLite database operations, schema management, all DB access via `Store` struct |
| `query` | DuckDB engine over Parquet files; SQLite fallback engine |
| `tui` | Bubble Tea TUI model, view rendering, navigation, selection, deletion staging |
| `sync` | Sync orchestration (Gmail/IMAP), MIME processing, checkpoint management |
| `gmail` | Gmail API client with rate limiting |
| `imap` | IMAP sync client |
| `oauth` | OAuth2 flows (browser and device/headless) |
| `mime` | MIME message parsing via enmime |
| `search` | Query parser for Gmail-like syntax |
| `config` | TOML config loading, path resolution, defaults |
| `api` | HTTP API handlers and middleware (chi router) |
| `mcp` | MCP server for AI agent integration |
| `deletion` | Deletion staging, manifest generation, execution |
| `importer` | Unified import logic (MBOX, EMLX ingestion) |
| `mbox` | MBOX file reader/parser |
| `emlx` | Apple Mail .emlx reader |
| `applemail` | Apple Mail account discovery |
| `export` | EML export and attachment extraction |
| `scheduler` | Cron-based scheduled sync (daemon mode) |
| `remote` | Remote server client (TUI to serve mode) |
| `textutil` | Charset detection and encoding conversion |
| `fileutil` | Secure file operations (chmod, mkdir, atomic writes) |
| `testutil` | Test helpers: dbtest, storetest, email builder |
| `update` | Self-update version checking |

## Data Flow Patterns

- **Ingest**: Gmail API / IMAP / MBOX / EMLX --> `sync` or `importer` --> `store` (SQLite)
- **Analytics**: `store` (SQLite) --> DuckDB ETL --> Parquet files --> `query` engine --> `tui`
- **Search**: `store` (FTS5) or `query` (Parquet) --> `search` parser --> CLI or TUI
- **API**: HTTP request --> `api` handlers --> `store` or `query` --> JSON response
- **MCP**: AI agent --> `mcp` server --> `store` or `query` --> structured response

## Schema Files

- `internal/store/schema.sql` -- Core unified schema (sources, conversations, messages, participants, attachments, labels, sync state)
- `internal/store/schema_sqlite.sql` -- FTS5 virtual table definition
- Schema is embedded via Go `embed` and applied on `init-db`

## Key Design Decisions

- **`message_bodies` is a separate table** from `messages` to keep the messages B-tree compact for fast scans. Only accessed by PK for single-message detail views.
- **Content-addressed attachments** (SHA-256) -- deduplicated across all messages and accounts.
- **Parquet partitioned by year** -- enables efficient time-range queries without scanning all data.
- **All DB access via `Store` struct** -- no direct SQL from command handlers.
- **Context-based cancellation** -- long operations (sync, import, cache build) respect `context.Context` for graceful shutdown.

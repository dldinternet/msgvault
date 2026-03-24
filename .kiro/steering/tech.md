# Technology Stack and Conventions

## Language and Runtime

- **Go 1.25+** (pinned to 1.25.8 in go.mod)
- **CGO required**: SQLite and DuckDB both use CGO bindings
- Build tag `-tags fts5` required for all build/test commands (enables SQLite FTS5)

## Core Dependencies

| Role | Library |
|------|---------|
| CLI framework | `spf13/cobra` |
| TUI | `charmbracelet/bubbletea` + `lipgloss` |
| SQLite | `mattn/go-sqlite3` (store, FTS5) |
| DuckDB | `marcboeker/go-duckdb` (Parquet analytics, cache building) |
| MIME parsing | `jhillyerd/enmime` |
| IMAP | `emersion/go-imap/v2` |
| OAuth2 | `golang.org/x/oauth2` |
| Config | `BurntSushi/toml` |
| HTTP API | `go-chi/chi/v5` |
| MCP server | `mark3labs/mcp-go` |
| Scheduler | `robfig/cron/v3` |
| Charset detection | `gogs/chardet` + `golang.org/x/text/encoding` |

## Build Commands

```
make build            # Debug build (CGO_ENABLED=1, -tags fts5)
make build-release    # Release build (stripped, trimpath)
make install          # Install to ~/.local/bin or GOPATH
make test             # go test -tags fts5 ./...
make lint             # golangci-lint run ./...
make fmt              # go fmt ./...
make setup-hooks      # Enable pre-commit hook (fmt + lint)
```

## Pre-Commit Discipline

Before every commit: `go fmt ./...` and `go vet ./...`. Stage ALL resulting changes including formatting-only files. A pre-commit hook enforces fmt + lint.

## Error Handling Pattern

Return `error` values, wrap with context: `fmt.Errorf("operation: %w", err)`. No panics in library code.

## Testing Conventions

- Table-driven tests as the default pattern
- Test files colocated with source (`foo_test.go` alongside `foo.go`)
- Test helpers in `internal/testutil/` (dbtest, storetest, email builder)
- All tests require `-tags fts5`

## SQL Rules (Critical)

1. **Never SELECT DISTINCT with JOINs** -- use EXISTS subqueries (semi-joins) instead
2. **Never JOIN or scan `message_bodies` in list/aggregate queries** -- this table is deliberately separated to keep the messages B-tree small. Access only via direct PK lookup for single-message detail views. Use FTS5 (`messages_fts`) for text search.

## Storage Architecture

Two-tier data model:

- **SQLite** (`msgvault.db`): System of record. All message metadata, raw MIME (zlib-compressed), FTS5 index. WAL mode, single connection via `Store` struct.
- **Parquet** (`analytics/`): Denormalized read-optimized cache for TUI analytics. Partitioned by year. Built from SQLite via DuckDB ETL. Auto-rebuilt on TUI launch when new messages exist.

## Configuration

TOML config at `~/.msgvault/config.toml`. Override home dir with `MSGVAULT_HOME` env var or `--home` flag. All data collocated under the home directory.

## Cross-Platform Support

macOS, Linux, Windows (including Git Bash). Path handling includes tilde expansion, Windows quote stripping, and fallback temp directory logic. Security-sensitive directories get 0700 permissions.

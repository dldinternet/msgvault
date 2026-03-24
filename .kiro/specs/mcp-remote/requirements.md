# Requirements: mcp-remote

## Project Description

Add remote mode support to the MCP server so it can connect to a remote msgvault API server (via [remote] config) instead of requiring a local SQLite database. Mirror the existing pattern from the TUI command which already supports remote mode via remote.Engine.

## Requirements

### 1. Remote Mode Engine Selection

The MCP server command shall use `remote.Engine` as the `query.Engine` when `[remote].url` is configured and the `--local` flag is not set.

**Acceptance Criteria:**

- When `[remote].url` is set in config and the `--local` flag is not provided, the MCP server shall create a `remote.Engine` using the configured URL, API key, and allow_insecure settings, and pass it to `mcpserver.Serve()`.
- When `[remote].url` is set in config and the `--local` flag is provided, the MCP server shall use the local SQLite database and Parquet/DuckDB engine as it does today.
- When `[remote].url` is not set in config, the MCP server shall use the local SQLite database and Parquet/DuckDB engine regardless of the `--local` flag.
- When `remote.NewEngine()` returns an error (e.g., invalid URL), the MCP command shall return that error without falling back to local mode.

### 2. Local Resource Handling in Remote Mode

The MCP server shall pass empty strings for `attachmentsDir` and `dataDir` when operating in remote mode.

**Acceptance Criteria:**

- When the MCP server operates in remote mode, the `attachmentsDir` argument to `mcpserver.Serve()` shall be an empty string.
- When the MCP server operates in remote mode, the `dataDir` argument to `mcpserver.Serve()` shall be an empty string.
- When the MCP server operates in local mode, the `attachmentsDir` and `dataDir` arguments shall be set to the configured local paths as they are today.

### 3. Read-Only Tool Functionality in Remote Mode

All read-only MCP tools shall function correctly when backed by a remote engine.

**Acceptance Criteria:**

- The `search_messages` tool shall return search results from the remote server when in remote mode. This tool calls `engine.SearchFast()` and falls back to `engine.Search()` -- both are implemented by `remote.Engine`.
- The `get_message` tool shall return full message details from the remote server when in remote mode via `engine.GetMessage()`.
- The `list_messages` tool shall return filtered message lists from the remote server when in remote mode via `engine.ListMessages()`.
- The `get_stats` tool shall return archive statistics from the remote server when in remote mode via `engine.GetTotalStats()` and `engine.ListAccounts()`.
- The `aggregate` tool shall return grouped statistics from the remote server when in remote mode via `engine.Aggregate()`.
- No code changes are needed in these handlers -- they work transparently through the `query.Engine` interface.

### 4. Graceful Degradation of Local-Only Tools

MCP tools that depend on local filesystem resources shall return informative error messages when those resources are unavailable in remote mode.

**Acceptance Criteria:**

- The `get_attachment` and `export_attachment` tools call `engine.GetAttachment()` before checking `attachmentsDir`. In remote mode, `remote.Engine.GetAttachment()` returns `remote.ErrNotSupported`, producing the error "get attachment failed: operation not supported in remote mode". This message is already remote-mode-aware and satisfies the informative error requirement. The `attachmentsDir == ""` guard serves as a belt-and-suspenders check but is not the primary error path in remote mode.
- The `stage_deletion` tool currently has no early guard for empty `dataDir`. A new guard (`if h.dataDir == ""`) shall be added at the top of the `stageDeletion` handler, before any search/filter logic, returning an error message such as "deletion staging is not available in remote mode". Without this guard, `deletion.NewManager("")` would fail with a confusing filesystem error.
- Each error message surfaced to the MCP client shall clearly indicate the operation is unavailable due to remote mode, not due to misconfiguration.

### 5. Diagnostic Output on Startup

The MCP server shall emit a diagnostic message to stderr indicating which mode it is operating in.

**Acceptance Criteria:**

- When the MCP server starts in remote mode, it shall print a message to stderr indicating it is connected to a remote server, including the server URL.
- When the MCP server starts in local mode, the existing behavior (no mode announcement or existing diagnostics) shall be preserved.
- The diagnostic message shall be written to stderr so it does not interfere with the stdio MCP protocol on stdout. Note: the TUI uses `fmt.Printf` (stdout) for its remote diagnostic, but the MCP server MUST use stderr because stdout carries the MCP stdio protocol.

### 6. Consistent Mode Resolution with Existing Commands

The MCP server shall use the same mode resolution logic as non-TUI commands.

**Acceptance Criteria:**

- The MCP server shall use the `IsRemoteMode()` helper from `store_resolver.go`, which checks the root command's `--local` persistent flag (`useLocal`) and `cfg.Remote.URL`. This matches the pattern used by `stats.go`, `search.go`, `show_message.go`, and `list_accounts.go`.
- Note: The TUI command uses its own local `forceLocalTUI` flag that shadows the root `--local` persistent flag. The MCP command shall NOT replicate this pattern -- it shall use `IsRemoteMode()` directly.
- The mode resolution order shall be: (1) `--local` flag forces local, (2) `[remote].url` configured uses remote, (3) default uses local.
- The `remote.Engine` shall be created with the same `remote.Config` fields (URL, APIKey, AllowInsecure) from `cfg.Remote`, consistent with the TUI command.

### 7. Resource Cleanup in Remote Mode

The MCP server shall properly close the remote engine when shutting down.

**Acceptance Criteria:**

- When the MCP server operates in remote mode, the `remote.Engine` shall be closed via `defer` when the command exits.
- When the MCP server operates in remote mode, it shall not open or attempt to close a local SQLite database.
- When the MCP server operates in local mode, existing cleanup behavior (closing the store and DuckDB engine) shall be preserved.

### 8. Unit Test Coverage

All new and modified code shall have unit test coverage of at least 80%.

**Acceptance Criteria:**

- Engine selection logic shall be testable. If the logic remains inline in the Cobra `RunE` closure (as in the TUI), an extracted helper function (e.g., `resolveMCPEngine(cfg, forceLocal, forceSQL, noSQLiteScanner) (query.Engine, attachmentsDir, dataDir, error)`) is recommended to enable direct unit testing. The design phase shall determine the exact refactoring approach.
- Tests shall verify engine selection for each mode: (a) remote URL set and `--local` not set -> remote engine, (b) remote URL set and `--local` set -> local engine, (c) remote URL not set -> local engine.
- Tests shall verify that `attachmentsDir` and `dataDir` are empty strings when in remote mode.
- Tests shall verify the new `stageDeletion` early guard returns the expected error when `dataDir` is empty.
- Tests shall verify that existing local mode behavior is unchanged.
- Overall statement coverage for changed files shall be at least 80%.

### 9. E2E Test Scenarios

End-to-end tests shall verify the MCP server operates correctly in remote mode against a test HTTP server.

**Acceptance Criteria:**

- An E2E test shall start a mock HTTP server implementing the msgvault API (using `httptest.Server`), configure the MCP server in remote mode, and verify that `search_messages` returns results from the mock server.
- An E2E test shall verify that `get_attachment` returns an error containing "not supported in remote mode" when the MCP server is in remote mode (reflecting the `remote.ErrNotSupported` from the engine).
- An E2E test shall verify that `stage_deletion` returns an error indicating remote mode unavailability when the MCP server is in remote mode.
- An E2E test shall verify that the MCP server starts and stops cleanly in remote mode without attempting to open a local database.
- Existing test infrastructure in `internal/mcp/server_test.go` (including `callToolDirect`, `runTool`, `runToolExpectError` helpers and `querytest.MockEngine`) shall be leveraged for these tests.

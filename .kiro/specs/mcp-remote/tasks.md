# Tasks: mcp-remote

## Task 1: Extract `resolveMCPEngine()` function with `mcpEngineResult` struct

Create the engine resolution function and result struct in `cmd/msgvault/cmd/mcp.go` that selects between remote and local engines based on configuration. Define the `mcpEngineResult` struct with `Engine`, `AttachmentsDir`, `DataDir`, `IsRemote`, and `Cleanup` fields. The function accepts `*config.Config`, `isRemote bool`, `forceSQL bool`, and `noSQLiteScanner bool`. When `isRemote` is true, create a `remote.Engine` using `cfg.Remote` fields (URL, APIKey, AllowInsecure), set `AttachmentsDir` and `DataDir` to empty strings, and set `Cleanup` to close the remote engine. When `isRemote` is false, preserve the existing local logic (open SQLite, init schema, start FTS backfill, select DuckDB or SQLite engine) and return local paths. Return errors from `remote.NewEngine()` without fallback to local mode. Use `IsRemoteMode()` from `store_resolver.go` as the caller passes the `isRemote` flag.

Requirements: 1, 2, 6

### Sub-tasks

- [x] Define `mcpEngineResult` struct with all fields including `Cleanup func() error`
- [x] Implement `resolveMCPEngine()` with remote branch creating `remote.Engine` and setting empty paths
- [x] Implement local branch preserving existing SQLite/DuckDB logic with proper cleanup composition
- [x] Add `remote` import to `mcp.go`

## Task 2: Update MCP command `RunE` to use `resolveMCPEngine()` and emit diagnostic

Replace the inline engine setup in `mcpCmd.RunE` with a call to `resolveMCPEngine(cfg, IsRemoteMode(), mcpForceSQL, mcpNoSQLiteScanner)`. Defer `result.Cleanup()`. When `result.IsRemote` is true, write a diagnostic message to stderr indicating the remote server URL (e.g., `"msgvault MCP: connected to remote server %s\n"`). Use `fmt.Fprintf(os.Stderr, ...)` so stdout remains reserved for the MCP stdio protocol. Preserve the existing `context.WithCancel` and `mcpserver.Serve` call, passing `result.Engine`, `result.AttachmentsDir`, and `result.DataDir`.

**Depends on**: Task 1 (calls `resolveMCPEngine()`)

Requirements: 5, 7

### Sub-tasks

- [x] Replace inline engine setup with `resolveMCPEngine()` call and `defer result.Cleanup()`
- [x] Add conditional stderr diagnostic for remote mode with server URL
- [x] Verify no local database is opened when in remote mode (no `store.Open` call in remote path)

## Task 3: Add `dataDir` early guard in `stageDeletion` handler (P)

Add a guard at the top of the `stageDeletion` method in `internal/mcp/handlers.go`, before any search or filter logic (before the `getAccountID` call). When `h.dataDir` is empty, return an MCP error result with the message `"deletion staging is not available in remote mode"`. This prevents `deletion.NewManager("")` from failing with a confusing filesystem error when operating against a remote server. This guard also short-circuits before `engine.GetGmailIDsByFilter()` which returns `remote.ErrNotSupported` in remote mode for structured filters.

Requirements: 4

### Sub-tasks

- [x] Add `if h.dataDir == ""` guard at the top of `stageDeletion`, returning `mcp.NewToolResultError("deletion staging is not available in remote mode")`
- [x] Verify the guard is placed before the `getAccountID` call so no engine methods are invoked

## Task 4: Write unit tests for `resolveMCPEngine()` engine selection logic

Create `cmd/msgvault/cmd/mcp_engine_test.go` with table-driven tests for the extracted `resolveMCPEngine()` function. Use `httptest.Server` to provide a valid URL for remote mode tests (avoiding CGO/SQLite dependency). Test cases: (a) remote URL set and `isRemote=true` returns a `*remote.Engine` with empty `AttachmentsDir` and `DataDir`, (b) invalid remote URL returns an error, (c) verify `IsRemote` flag is set correctly in the result. For local mode, test that `isRemote=false` with a valid SQLite database returns a non-nil engine with non-empty paths. Ensure cleanup functions are called without error.

**Depends on**: Task 1 (tests the function created in Task 1)

Requirements: 8

### Sub-tasks

- [x] Create test file with table-driven tests covering remote mode engine selection
- [x] Test that `AttachmentsDir` and `DataDir` are empty strings in remote mode
- [x] Test that invalid remote URL returns an error without fallback
- [x] Test cleanup function executes without error
- [x] (CGO-only) Add local mode test case verifying non-empty paths and correct engine type

## Task 5: Add `stageDeletion` remote-mode guard test and E2E remote-mode tests

Extend `internal/mcp/server_test.go` with: (a) a unit test confirming `stageDeletion` with empty `dataDir` returns an error containing "remote mode", (b) E2E tests using `httptest.Server` as a mock remote API. The E2E tests create a `remote.Engine` pointed at the mock server, construct `handlers` with that engine and empty paths, then exercise MCP tools through the existing `callToolDirect`/`runTool`/`runToolExpectError` helpers. Verify: `search_messages` returns results from the mock server, `get_attachment` returns an error containing "not supported in remote mode" (the exact text from `remote.ErrNotSupported` formatted as `"get attachment failed: operation not supported in remote mode"`), `stage_deletion` returns an error indicating remote mode unavailability, and the remote engine starts and closes cleanly.

Note: The `search_messages` E2E test also validates Requirement 3 (read-only tools work transparently through `query.Engine` in remote mode).

**Depends on**: Task 3 (stageDeletion guard test verifies Task 3's implementation)

Requirements: 3, 8, 9

### Sub-tasks

- [x] Add test for `stageDeletion` with `dataDir: ""` asserting error contains "remote mode"
- [x] Create mock HTTP server implementing `/api/v1/search/fast` and `/api/v1/accounts` for E2E tests
- [x] Add E2E test verifying `search_messages` returns results through remote engine (validates R3)
- [x] Add E2E test verifying `get_attachment` returns error containing "get attachment failed: operation not supported in remote mode"
- [x] Add E2E test verifying `stage_deletion` returns error containing "deletion staging is not available in remote mode"
- [x] Add E2E test verifying remote engine creates and closes without errors

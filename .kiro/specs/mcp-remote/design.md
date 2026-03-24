# Design: mcp-remote

## Overview

Add remote mode support to the MCP server command so it can connect to a remote msgvault API server instead of requiring a local SQLite database. The implementation mirrors the existing remote mode pattern established by the TUI command and other CLI commands, using `remote.Engine` behind the `query.Engine` interface.

### Architecture Diagram

```
                   MCP Client (Claude Desktop / stdio)
                              |
                         [stdin/stdout]
                              |
                     cmd/msgvault/cmd/mcp.go
                        resolveMCPEngine()
                         /            \
                   [remote]          [local]
                      |                 |
               remote.Engine     DuckDB or SQLite
                      |            query.Engine
               HTTP Client              |
                      |          local SQLite DB
               Remote Server     + Parquet files
```

### Requirements Traceability

| Requirement | Component(s) | Section |
|---|---|---|
| 1 (Remote Mode Engine Selection) | `resolveMCPEngine()` in `mcp.go` | 1, 2 |
| 2 (Local Resource Handling) | `resolveMCPEngine()` return values | 1 |
| 3 (Read-Only Tool Functionality) | No changes -- transparent via `query.Engine` | 3 |
| 4 (Graceful Degradation of Local-Only Tools) | `stageDeletion` guard in `handlers.go` | 4 |
| 5 (Diagnostic Output on Startup) | `mcp.go` stderr message | 5 |
| 6 (Consistent Mode Resolution) | `IsRemoteMode()` usage in `resolveMCPEngine()` | 2 |
| 7 (Resource Cleanup) | `defer` in `mcp.go` remote branch | 6 |
| 8 (Unit Test Coverage) | `mcp_engine_test.go`, `server_test.go` additions | 7 |
| 9 (E2E Test Scenarios) | `server_test.go` remote mode tests | 7 |

---

## Component Design

### 1. Engine Resolution Function

**File**: `cmd/msgvault/cmd/mcp.go`

Extract the engine selection logic from the `RunE` closure into a standalone function to enable direct unit testing (requirement 8).

```go
// mcpEngineResult holds the resolved engine and associated resource paths.
type mcpEngineResult struct {
    Engine         query.Engine
    AttachmentsDir string
    DataDir        string
    IsRemote       bool
    Cleanup        func() error // caller must defer this
}
```

```go
// resolveMCPEngine selects the appropriate query engine based on configuration.
// When IsRemoteMode() returns true, it creates a remote.Engine and sets
// AttachmentsDir and DataDir to empty strings.
// When local, it opens SQLite, initializes schema, starts FTS backfill,
// and selects DuckDB/Parquet or SQLite engine.
func resolveMCPEngine(
    cfg *config.Config,
    isRemote bool,
    forceSQL bool,
    noSQLiteScanner bool,
) (*mcpEngineResult, error)
```

**Return behavior**:

| Condition | Engine | AttachmentsDir | DataDir | Cleanup |
|---|---|---|---|---|
| `isRemote == true` | `remote.Engine` | `""` | `""` | `remoteEngine.Close` |
| `isRemote == false`, Parquet available | `query.DuckDBEngine` | `cfg.AttachmentsDir()` | `cfg.Data.DataDir` | `duckEngine.Close` + `store.Close` |
| `isRemote == false`, Parquet unavailable | `query.SQLiteEngine` | `cfg.AttachmentsDir()` | `cfg.Data.DataDir` | `store.Close` |
| `isRemote == false`, DuckDB fails | `query.SQLiteEngine` (fallback) | `cfg.AttachmentsDir()` | `cfg.Data.DataDir` | `store.Close` |

**Error behavior**: When `remote.NewEngine()` returns an error (invalid URL, HTTPS enforcement), the function returns that error without fallback to local mode. This matches the TUI pattern and satisfies requirement 1.

### 2. Updated MCP Command

**File**: `cmd/msgvault/cmd/mcp.go`

The `RunE` closure calls `resolveMCPEngine()` and passes the result to `mcpserver.Serve()`.

```go
RunE: func(cmd *cobra.Command, args []string) error {
    result, err := resolveMCPEngine(cfg, IsRemoteMode(), mcpForceSQL, mcpNoSQLiteScanner)
    if err != nil {
        return err
    }
    defer result.Cleanup()

    if result.IsRemote {
        fmt.Fprintf(os.Stderr, "msgvault MCP: connected to remote server %s\n", cfg.Remote.URL)
    }

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    return mcpserver.Serve(ctx, result.Engine, result.AttachmentsDir, result.DataDir)
}
```

**Key points**:
- Uses `IsRemoteMode()` from `store_resolver.go` (requirement 6), not a local `forceLocalTUI`-style flag.
- Diagnostic output writes to stderr (requirement 5) because stdout carries the MCP stdio protocol.
- No new flags needed -- the root `--local` persistent flag is already available.
- The `Cleanup` function consolidates all deferred close operations into a single callable.

### 3. Read-Only Tools (No Changes)

**Files**: No modifications.

The five read-only MCP tools (`search_messages`, `get_message`, `list_messages`, `get_stats`, `aggregate`) work transparently through the `query.Engine` interface. `remote.Engine` implements all methods these tools call:

| Tool | Engine Methods | Remote Implementation |
|---|---|---|
| `search_messages` | `SearchFast()`, `Search()` | HTTP proxy to `/api/v1/search/fast`, `/api/v1/search/deep` |
| `get_message` | `GetMessage()` | HTTP proxy to `/api/v1/messages/{id}` |
| `list_messages` | `ListMessages()` | HTTP proxy to `/api/v1/messages/filter` |
| `get_stats` | `GetTotalStats()`, `ListAccounts()` | HTTP proxy to `/api/v1/stats/total`, `/api/v1/accounts` |
| `aggregate` | `Aggregate()` | HTTP proxy to `/api/v1/aggregates` |

No code changes required -- this satisfies requirement 3.

### 4. Graceful Degradation Guard for `stageDeletion`

**File**: `internal/mcp/handlers.go`

Add an early guard at the top of the `stageDeletion` handler, before any search or filter logic:

```go
func (h *handlers) stageDeletion(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    if h.dataDir == "" {
        return mcp.NewToolResultError("deletion staging is not available in remote mode"), nil
    }
    // ... existing code unchanged ...
}
```

**Rationale**: Without this guard, `deletion.NewManager("")` would fail with a confusing filesystem error when `dataDir` is empty. The guard provides a clear, actionable error message (requirement 4).

**Attachment tools (`getAttachment`, `exportAttachment`)**: No changes needed. In remote mode, `engine.GetAttachment()` is called first and returns `remote.ErrNotSupported`. The handler catches this and returns `"get attachment failed: operation not supported in remote mode"`, which is already informative and remote-mode-aware. The `attachmentsDir == ""` check acts as a belt-and-suspenders fallback but is not the primary error path.

### 5. Diagnostic Output

**File**: `cmd/msgvault/cmd/mcp.go`

When operating in remote mode, print a single line to stderr:

```
msgvault MCP: connected to remote server https://nas:8080
```

When operating in local mode, no diagnostic is printed (preserving existing behavior). The message uses `fmt.Fprintf(os.Stderr, ...)` to avoid polluting the MCP stdio protocol on stdout (requirement 5).

### 6. Resource Cleanup

**File**: `cmd/msgvault/cmd/mcp.go`

The `mcpEngineResult.Cleanup` function encapsulates all deferred close operations:

| Mode | Cleanup actions |
|---|---|
| Remote | `remoteEngine.Close()` |
| Local (DuckDB) | `duckEngine.Close()`, `store.Close()` |
| Local (SQLite) | `store.Close()` |

In remote mode, no local SQLite database is opened and no FTS backfill is started (requirement 7). The `Cleanup` function is called via `defer` in the `RunE` closure.

### 7. Test Strategy

#### 7.1 Unit Tests for Engine Resolution

**File**: `cmd/msgvault/cmd/mcp_engine_test.go` (new file)

Test the extracted `resolveMCPEngine()` function directly with table-driven tests:

| Test Case | isRemote | forceSQL | Expected Engine Type | AttachmentsDir | DataDir |
|---|---|---|---|---|---|
| Remote mode | `true` | `false` | `*remote.Engine` | `""` | `""` |
| Local + Parquet | `false` | `false` | `*query.DuckDBEngine` or `*query.SQLiteEngine` | non-empty | non-empty |
| Local + forceSQL | `false` | `true` | `*query.SQLiteEngine` | non-empty | non-empty |
| Remote URL invalid | `true` (bad URL) | `false` | error | -- | -- |

Testing local mode requires a real SQLite database (CGO). The remote mode tests can use `httptest.Server` to provide a valid URL for `remote.NewEngine()` without needing CGO or a real database.

The `resolveMCPEngine` function takes `*config.Config` and boolean flags as parameters, making it straightforward to construct test configs without Cobra command infrastructure.

#### 7.2 Handler Tests for `stageDeletion` Guard

**File**: `internal/mcp/server_test.go`

Add a test case using the existing test infrastructure:

```go
t.Run("remote mode rejects staging", func(t *testing.T) {
    h := &handlers{engine: &querytest.MockEngine{}, dataDir: ""}
    r := runToolExpectError(t, "stage_deletion", h.stageDeletion, map[string]any{
        "from": "test@example.com",
    })
    txt := resultText(t, r)
    // Assert error mentions "remote mode"
})
```

#### 7.3 E2E Tests for Remote Mode

**File**: `internal/mcp/server_test.go`

E2E tests use `httptest.Server` to mock the remote msgvault API, then create a `remote.Engine` pointed at the mock server and exercise the MCP handlers through the existing test helpers:

1. **Search through remote**: Mock `/api/v1/search/fast` to return results, verify `search_messages` tool returns them.
2. **Attachment error in remote mode**: Create handlers with `remote.Engine` (mock returns `ErrNotSupported`), verify `get_attachment` returns error containing "not supported in remote mode".
3. **Stage deletion error in remote mode**: Create handlers with `dataDir: ""`, verify `stage_deletion` returns error containing "remote mode".
4. **Clean startup/shutdown**: Verify `remote.Engine` can be created from mock server and closed without errors.

The mock HTTP server needs to implement only the specific endpoints called by each test (e.g., `/api/v1/search/fast` for search tests, `/api/v1/accounts` for account lookup). The `httptest.Server` provides a valid URL with the `http` scheme; tests pass `AllowInsecure: true` in the `remote.Config`.

---

## Files Changed

| File | Change Type | Description |
|---|---|---|
| `cmd/msgvault/cmd/mcp.go` | Modified | Add `resolveMCPEngine()` function, update `RunE` to use it, add `remote` import, add stderr diagnostic |
| `internal/mcp/handlers.go` | Modified | Add `dataDir == ""` early guard in `stageDeletion` (3 lines) |
| `cmd/msgvault/cmd/mcp_engine_test.go` | New | Unit tests for `resolveMCPEngine()` engine selection logic |
| `internal/mcp/server_test.go` | Modified | Add `stageDeletion` remote-mode guard test, add E2E tests with mock HTTP server |

**Estimated scope**: ~250 lines across 4 files.

---

## Design Decisions

### D1: Extract `resolveMCPEngine()` vs Inline (like TUI)

**Decision**: Extract into a standalone function.

**Rationale**: Requirement 8 mandates 80% unit test coverage. The TUI's inline pattern is not directly testable without running the full Cobra command. Extracting the logic into a function with explicit parameters (`*config.Config`, `isRemote bool`, `forceSQL bool`, `noSQLiteScanner bool`) enables table-driven unit tests. The function returns a result struct rather than multiple values for clarity and to bundle the cleanup function.

### D2: No `isRemote` Field on Handlers Struct

**Decision**: Do not add an `isRemote` field to the `handlers` struct.

**Rationale**: The gap analysis proposed this for differentiated error messages, but closer inspection reveals it is unnecessary. For attachment tools, `remote.Engine.GetAttachment()` returns `ErrNotSupported` before the `attachmentsDir == ""` guard is reached, producing the already-informative message "get attachment failed: operation not supported in remote mode". For `stageDeletion`, a `dataDir == ""` guard with an explicit remote-mode error message is sufficient. Adding `isRemote` to `handlers` would also require changing the `Serve()` function signature, which is unnecessary complexity.

### D3: Keep `Serve()` Signature Unchanged

**Decision**: No changes to `mcpserver.Serve(ctx, engine, attachmentsDir, dataDir)`.

**Rationale**: The function already accepts `query.Engine` as an interface, so `remote.Engine` plugs in transparently. Empty strings for `attachmentsDir` and `dataDir` signal remote mode to the handlers. No additional parameter is needed.

### D4: Use `IsRemoteMode()` (not TUI-style Local Flag)

**Decision**: Use the root command's `IsRemoteMode()` helper from `store_resolver.go`.

**Rationale**: The TUI defines its own `forceLocalTUI` local flag that shadows the root's `--local` persistent flag. This is a historical inconsistency. The MCP command follows the canonical pattern used by `stats.go`, `search.go`, `show_message.go`, and `list_accounts.go`, which all use `IsRemoteMode()` checking the root `useLocal` persistent flag. No new flags are needed.

# Gap Analysis: mcp-remote

## Analysis Summary

- **Scope**: Add remote mode to the MCP server command (`mcp.go`) so it can proxy queries to a remote msgvault HTTP API instead of requiring a local SQLite/DuckDB setup. All 9 requirements are achievable with the existing codebase infrastructure.
- **Primary Finding**: The TUI command (`tui.go`) already implements the exact same pattern (remote engine selection, `--local` override, empty-dir guards for local-only features). The MCP command can directly mirror this with minimal new code -- roughly 30-40 lines of changes in `mcp.go` and 5-10 lines of error message improvements in `handlers.go`.
- **Key Gap**: The current `mcp.go` has zero remote-awareness; it unconditionally opens a local SQLite database and passes local filesystem paths for attachments/data. The handlers already partially guard against empty `attachmentsDir` but the error messages are generic ("attachments directory not configured") rather than remote-mode-specific as required.
- **Integration Risk**: Low. The `remote.Engine` already implements `query.Engine` and is battle-tested through the TUI. The `mcpserver.Serve()` function accepts `query.Engine` as an interface -- no signature changes needed.
- **Flag Inconsistency**: The TUI uses its own `forceLocalTUI` local flag while the root command provides a `useLocal` persistent flag with `IsRemoteMode()` helper. The MCP command should use the root-level `useLocal` / `IsRemoteMode()` approach for consistency with other commands (`stats.go`, `search.go`, `list_accounts.go`).

---

## 1. Existing Codebase Analysis

### 1.1 Current MCP Command (`cmd/msgvault/cmd/mcp.go`)

The current implementation is local-only:

1. Opens SQLite via `store.Open(cfg.DatabaseDSN())`
2. Initializes schema and starts FTS backfill
3. Selects DuckDB/Parquet or SQLite engine
4. Calls `mcpserver.Serve(ctx, engine, cfg.AttachmentsDir(), cfg.Data.DataDir)`

There is no reference to `remote`, `IsRemoteMode`, `useLocal`, or any remote configuration. All 4 arguments to `Serve()` assume local resources.

### 1.2 Reference Implementation: TUI Command (`cmd/msgvault/cmd/tui.go`)

The TUI already implements the exact remote mode pattern needed:

```
if cfg.Remote.URL != "" && !forceLocalTUI {
    remoteCfg := remote.Config{URL, APIKey, AllowInsecure}
    remoteEngine, err := remote.NewEngine(remoteCfg)
    defer remoteEngine.Close()
    engine = remoteEngine
    isRemote = true
} else {
    // local mode: open DB, init schema, build cache, select engine
}
```

The TUI then passes `isRemote` to control feature availability (disabling deletion/export in remote mode).

### 1.3 Remote Engine (`internal/remote/engine.go`)

`remote.Engine` implements `query.Engine` fully. Key behaviors in remote mode:
- Read-only tools work: `Aggregate`, `ListMessages`, `GetMessage`, `Search`, `SearchFast`, `GetTotalStats`, `ListAccounts` -- all proxy to HTTP API endpoints
- Unsupported operations return `remote.ErrNotSupported`: `GetMessageBySourceID`, `GetAttachment`, `GetGmailIDsByFilter`

### 1.4 MCP Handlers (`internal/mcp/handlers.go`)

The handlers struct holds `engine`, `attachmentsDir`, and `dataDir`. Existing guards:

| Handler | Guard for empty dir | Current error message |
|---------|--------------------|-----------------------|
| `getAttachment` | `h.attachmentsDir == ""` | "attachments directory not configured" |
| `exportAttachment` | `h.attachmentsDir == ""` | "attachments directory not configured" |
| `stageDeletion` | Uses `h.dataDir` implicitly | Fails at `deletion.NewManager(deletionsDir)` |

**Gap**: Error messages are generic, not remote-mode-aware. Requirement 4 asks for messages like "attachments are not available in remote mode" rather than the current "not configured" phrasing.

### 1.5 Mode Resolution (`cmd/msgvault/cmd/store_resolver.go`)

`IsRemoteMode()` checks `useLocal` flag (root persistent flag) and `cfg.Remote.URL`. Used by `stats.go`, `search.go`, `list_accounts.go`, `show_message.go`. This is the canonical pattern.

### 1.6 Config (`internal/config/config.go`)

`RemoteConfig` struct with `URL`, `APIKey`, `AllowInsecure` fields already exists in `Config.Remote`. No config changes needed.

---

## 2. Gap Identification

### Gap 1: mcp.go lacks remote engine selection (Requirements 1, 6)

**Current**: Always opens local SQLite and creates local engine.
**Needed**: Branch on `IsRemoteMode()` to create `remote.Engine` instead.
**Effort**: Small. Directly mirror the TUI pattern.

### Gap 2: mcp.go passes local paths in remote mode (Requirement 2)

**Current**: Always passes `cfg.AttachmentsDir()` and `cfg.Data.DataDir`.
**Needed**: Pass empty strings `""` for both when in remote mode.
**Effort**: Trivial. Two conditional assignments.

### Gap 3: Handlers lack remote-mode-specific error messages (Requirement 4)

**Current**: `getAttachment` and `exportAttachment` check `h.attachmentsDir == ""` but report "attachments directory not configured". `stageDeletion` would fail cryptically at the deletion manager level.
**Needed**: Clear messages stating the operation is unavailable because the server is connected to a remote instance.
**Effort**: Small. Two options:
  - **Option A**: Change the error message strings in `handlers.go` to be remote-aware when the dir is empty. This works because empty dirs are *only* set in remote mode, so the message can be unconditionally "not available in remote mode".
  - **Option B**: Add an `isRemote` field to the `handlers` struct and branch on it for different error messages. More explicit but slightly more code.

### Gap 4: No stderr diagnostic on startup (Requirement 5)

**Current**: No mode announcement.
**Needed**: Print to stderr in remote mode (must not pollute stdout which carries the MCP stdio protocol).
**Effort**: Trivial. One `fmt.Fprintf(os.Stderr, ...)` call.

### Gap 5: No cleanup of remote engine (Requirement 7)

**Current**: Only defers close of local store and DuckDB engine.
**Needed**: `defer remoteEngine.Close()` in the remote branch.
**Effort**: Trivial. Already part of the branching pattern.

### Gap 6: `stageDeletion` needs an explicit early-return guard (Requirement 4)

**Current**: `stageDeletion` handler does not check `h.dataDir` before reaching `deletion.NewManager()`. In remote mode with empty `dataDir`, `NewManager("")` would fail with a confusing filesystem error.
**Needed**: Add an early guard `if h.dataDir == ""` with a clear remote-mode error message.
**Effort**: Small. 3-4 lines.

### Gap 7: No unit tests for remote mode branching in mcp.go (Requirement 8)

**Current**: `server_test.go` tests handler logic with mock engines but does not test the command-level engine selection.
**Needed**: Tests verifying:
  - When `cfg.Remote.URL` is set and `useLocal` is false, remote engine is selected
  - When `cfg.Remote.URL` is set and `useLocal` is true, local engine is selected
  - When `cfg.Remote.URL` is empty, local engine is used
  - `attachmentsDir` and `dataDir` are empty in remote mode
**Effort**: Medium. Requires either refactoring the `RunE` closure to be testable (extract an engine-selection function) or testing via integration approach.

### Gap 8: No E2E tests for MCP in remote mode (Requirement 9)

**Current**: No E2E test infrastructure for MCP server.
**Needed**: Tests starting a mock HTTP server, configuring MCP in remote mode, and verifying tool calls flow through.
**Effort**: Medium. Can leverage existing `httptest.Server` patterns from the API test suite and the MCP test helpers.

---

## 3. Implementation Options

### Option A: Minimal Inline Change (Recommended)

Modify `mcp.go` inline, mirroring the TUI pattern:

- Add `if IsRemoteMode()` branch at the top of `RunE`
- In remote branch: create `remote.Engine`, set `attachmentsDir=""`, `dataDir=""`, print diagnostic to stderr
- In local branch: existing code unchanged
- Update handler error messages in `handlers.go`

**Pros**: Minimal diff, follows existing conventions, easy to review.
**Cons**: Engine-selection logic lives in the command file (same as TUI), not easily unit-testable without refactoring.

### Option B: Extract Engine Factory

Create a shared function like `resolveEngine(cfg, forceLocal, forceSQL, noSQLiteScanner) (query.Engine, string, string, error)` that both TUI and MCP call:

**Pros**: DRY, testable engine selection, consistent behavior guaranteed.
**Cons**: More refactoring, touches TUI code (risk of regression), premature generalization if only 2 callers.

### Option C: Add `isRemote` Field to MCP Handlers

Extend the `handlers` struct with an `isRemote bool` field, use it for differentiated error messages:

**Pros**: Explicit, allows handlers to distinguish "not configured" from "remote mode".
**Cons**: Slightly more code; arguably unnecessary since empty dir paths are a sufficient signal for remote mode.

**Recommendation**: Option A for the command-level change, combined with Option C's `isRemote` field for handler error messages. This gives the best clarity in error messages without over-engineering.

---

## 4. Integration Points

### 4.1 Imports Needed in mcp.go

```go
import "github.com/wesm/msgvault/internal/remote"
```

No changes to `internal/mcp/server.go` signature -- `Serve()` already accepts `query.Engine` interface.

### 4.2 Flag Handling

The root command already provides `--local` as a persistent flag bound to `useLocal`. The `IsRemoteMode()` function is available. No new flags needed for `mcp.go`.

Note: The TUI's `forceLocalTUI` local flag shadows the root's `useLocal` persistent flag. The MCP command should use `IsRemoteMode()` directly (which checks `useLocal`), matching the pattern in `stats.go`, `search.go`, etc. This avoids the TUI's flag duplication issue.

### 4.3 Handler Changes

The `handlers` struct in `internal/mcp/handlers.go` needs:
1. An `isRemote bool` field (optional, for clearer error messages)
2. Updated error messages in `getAttachment`, `exportAttachment`, and `stageDeletion`

The `Serve()` function signature does not need to change -- the empty string convention for `attachmentsDir`/`dataDir` is sufficient. However, if the `isRemote` field approach is used, `Serve()` would need an additional parameter or an options struct.

### 4.4 Existing Test Infrastructure

- `querytest.MockEngine` fully implements `query.Engine` -- usable for both unit and E2E tests
- `server_test.go` has mature test helpers (`callToolDirect`, `runTool`, `runToolExpectError`)
- `httptest.Server` from stdlib available for mock remote server in E2E tests

---

## 5. Read-Only Tool Compatibility (Requirement 3)

All 5 read-only MCP tools use `query.Engine` methods that `remote.Engine` implements:

| MCP Tool | Engine Method(s) | Remote Support |
|----------|-----------------|----------------|
| `search_messages` | `SearchFast`, `Search` | Yes |
| `get_message` | `GetMessage` | Yes |
| `list_messages` | `ListMessages` | Yes |
| `get_stats` | `GetTotalStats`, `ListAccounts` | Yes |
| `aggregate` | `Aggregate` | Yes |

No code changes needed in these handlers -- they work transparently through the `query.Engine` interface.

---

## 6. Local-Only Tool Degradation (Requirement 4)

Three tools need graceful degradation:

| MCP Tool | Dependency | Current Guard | Needed Change |
|----------|-----------|---------------|---------------|
| `get_attachment` | `attachmentsDir`, `GetAttachment()` | Checks `attachmentsDir == ""` | Better error message |
| `export_attachment` | `attachmentsDir`, `GetAttachment()` | Checks `attachmentsDir == ""` | Better error message |
| `stage_deletion` | `dataDir` | No check | Add `dataDir == ""` guard |

Note: `get_attachment` and `export_attachment` also call `engine.GetAttachment()`, which returns `remote.ErrNotSupported` in remote mode. The handler catches this error but formats it as "get attachment failed: operation not supported in remote mode". This is informative but could be improved. The `attachmentsDir == ""` check happens AFTER the `GetAttachment` call, so the `ErrNotSupported` error would surface first. This is actually acceptable behavior -- the engine-level error is clear enough. The `attachmentsDir` guard is a belt-and-suspenders check.

---

## 7. Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| MCP stdout pollution from diagnostic messages | Low | High (breaks stdio protocol) | Use `fmt.Fprintf(os.Stderr, ...)` exclusively |
| TUI flag pattern duplication | Low | Low | Use `IsRemoteMode()` from root, document difference |
| Remote engine connection failure at startup | Medium | Medium | Return clear error from `remote.NewEngine()` (already handles this) |
| `stageDeletion` fails cryptically in remote mode | High (without fix) | Medium | Add explicit `dataDir == ""` guard |

---

## 8. Areas Needing Further Research

1. **E2E Test Strategy**: Determine whether to use `httptest.Server` directly or leverage the existing `api` package handlers to create a realistic mock server. The API handler tests in `internal/api/` may provide reusable patterns.

2. **Unit Test Refactoring**: The command-level engine selection logic in `mcp.go`'s `RunE` closure is hard to unit test without refactoring. Consider extracting a helper function that takes config values and returns `(query.Engine, attachmentsDir, dataDir, error)` -- but this should be decided in the design phase based on code complexity.

3. **Serve() Signature**: Decide whether to pass `isRemote` to `Serve()` (cleaner error messages) or keep the current signature and infer remote mode from empty paths (simpler but less explicit).

---

## 9. Estimated Scope

| Area | Files Changed | Lines (est.) | Complexity |
|------|--------------|-------------|------------|
| Remote engine selection | `cmd/msgvault/cmd/mcp.go` | ~30 | Low |
| Handler error messages | `internal/mcp/handlers.go` | ~10 | Low |
| Serve function (optional) | `internal/mcp/server.go` | ~5 | Low |
| Unit tests | `internal/mcp/server_test.go` | ~80 | Medium |
| E2E tests | New file or extension | ~120 | Medium |
| **Total** | **3-4 files** | **~245** | **Low-Medium** |

# Product Context

## What msgvault Is

An offline email archive tool. Single Go binary that downloads a complete local copy of email from Gmail, IMAP servers, MBOX exports, and Apple Mail directories, then provides search, analytics, and AI access entirely offline.

## Who It's For

Individual users who want ownership of their email data -- decades of correspondence, attachments, and history -- independent of any web interface or API. The project is alpha software by wesm, open source under MIT.

## Core Value Proposition

- **Data sovereignty**: Your messages belong to you, stored locally, no network required after sync
- **Speed**: Millisecond analytics over hundreds of thousands of messages via DuckDB/Parquet
- **Completeness**: Raw MIME, attachments, labels, metadata -- nothing lost in archival
- **Safety-first deletion**: Stage, review, then execute Gmail deletions only after verifying the local archive

## Primary Workflows

1. **Archive**: Sync email from Gmail/IMAP, import from MBOX/Apple Mail
2. **Explore**: Interactive TUI with drill-down analytics (senders, domains, labels, time)
3. **Search**: Full-text search with Gmail-like query syntax (`from:`, `has:attachment`, date ranges)
4. **AI integration**: MCP server lets AI assistants query the full archive conversationally
5. **Delete**: Stage messages for deletion, generate manifests, execute via Gmail API
6. **Serve**: Daemon mode with scheduled syncs and HTTP API for remote TUI access

## Design Principles

- **Read-only by default**: Sync never modifies the email source; deletion is a separate, explicit workflow
- **Offline-first**: All features work without network access after initial sync
- **Resumable**: Interrupted syncs resume from the last checkpoint; no re-downloading
- **Multi-account**: Single database archives multiple Gmail and IMAP accounts
- **Single binary**: No runtime dependencies, no containers, no external databases

## What It Is Not

- Not a mail client (no sending, no composing)
- Not a cloud service (everything runs locally)
- Not yet encrypted at rest (planned but not implemented)
- Not yet available as a web UI (TUI only, plus MCP for AI agents)

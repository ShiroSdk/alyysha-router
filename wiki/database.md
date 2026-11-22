---
title: Database
order: 11
section: Under the Hood
---

# Database

Alyysha Router can open an upstream 9Router SQLite database without a manual
migration script. Bootstrap adds missing tables and columns. Dashboard
import/export and automatic backups are separate features; they do not mean
that every export is a complete database snapshot.

## Location

| | Path |
|---|---|
| macOS / Linux | `~/.9router/db/data.sqlite` |
| Windows | `%APPDATA%\9router\db\data.sqlite` |
| Docker | `/data/db/data.sqlite` inside the container |

## Sharing with 9Router

Stop 9Router, run Alyysha Router, and log in with your existing password. Your
provider connections, proxy pools, combos, API keys, model aliases, and usage
history are all there — Alyysha Router's dashboard renders them because it *is* the
same data.

Use one active gateway writer per database. Stop the legacy gateway before
switching; changing only the port does not isolate SQLite or settings writes.
For side-by-side evaluation, use a separate `DATA_DIR` and a stopped, copied
snapshot, never the live database.

Alyysha Router adds more than the original connection-rotation columns, including
governance, dashboard-token, settings-history, and combo-budget storage. Do not
assume a reverse switch to an arbitrary upstream version preserves every
Alyysha Router-specific field. Keep a pre-switch snapshot and consult the current
[`DATABASE.md`](https://github.com/yanstd888/9routerx/blob/main/DATABASE.md)
operator contract.

## What it does not do

- **No legacy JSON import.** If your 9Router data still lives in old JSON files,
  start upstream once to convert them, then switch.
- **No destructive migrations or pre-migration backups.** Its bootstrap only ever
  *adds* — create missing tables, add missing columns, seed missing rows.
- **No interpretation of `_meta.schemaVersion`.** That is upstream's migration
  bookkeeping. A database written by a much newer 9Router release should be
  checked before you rely on it.

## Backup

Stop the daemon before making a filesystem copy of the database directory,
including its WAL sidecars:

```bash
cp -r ~/.9router/db ~/9router-db-backup
```

`GET /api/settings/database` exports dashboard JSON; `POST` imports it. This
format is a data transfer, not a full snapshot of every table/column. In
particular, the current JSON export omits combo-budget columns and auto-backup
settings. Preserve a SQLite snapshot when those fields or full recovery matter.

The automatic backup path creates a primary SQLite snapshot and a best-effort
JSON companion. Remote Telegram/GitHub delivery prefers sensitive JSON and
falls back to the full SQLite snapshot when JSON is empty or fails. Neither
artifact is encrypted as a SyncBundle; the configured `.json` destination can
still contain SQLite bytes, so inspect the format before restoring. Browser-profile
directories are outside these built-in database backups. See
[Operations](operations.md#automatic-and-remote-backups) for settings,
retention, and restore limitations.

## Binary portability

One binary per target, built from pure Go source with `CGO_ENABLED=0`. Every
release target must still build — that is the only thing constraining a
dependency here, not its size.

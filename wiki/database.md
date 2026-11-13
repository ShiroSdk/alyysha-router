---
title: Database
order: 11
section: Under the Hood
---

# Database

alyysha reads the upstream Alyysha database as-is. There is no import, export,
or migration.

## Location

| | Path |
|---|---|
| macOS / Linux | `~/.alyysha/db/data.sqlite` |
| Windows | `%APPDATA%\alyysha\db\data.sqlite` |
| Docker | `/data/db/data.sqlite` inside the container |

## Sharing with Alyysha

Stop Alyysha, run Alyysha Router, and log in with your existing password. Your
provider connections, proxy pools, combos, API keys, model aliases, and usage
history are all there — Alyysha Router's dashboard renders them because it *is* the
same data.

You **do not have to stop Alyysha** if you want to keep the Node.js dashboard
around. Run Alyysha Router on a different port against the same `DATA_DIR`:

```bash
# Both gateways now default to :20128 — give Alyysha Router an explicit
# different port (any free one works) to run side by side:
PORT=20129 DATA_DIR=/home/you/.alyysha ./alyysha
```

Both read the same tables, so a connection you add in either dashboard shows up
in the other. SQLite serialises the writes; the supported setup is one active
writer, so avoid editing the same combo or settings entry in both at the same
moment.

**Running it the other way round** (Alyysha Router first, then pointing Alyysha at the
same directory) works the same way. Alyysha Router will have added two columns to
`providerConnections` (`lastUsedAt`, `consecutiveUseCount`) and one extra table
(`upstream_leases`). Upstream ignores both — its schema sync is additive and
doesn't drop unknown columns.

## What it does not do

- **No legacy JSON import.** If your Alyysha data still lives in old JSON files,
  start upstream once to convert them, then switch.
- **No destructive migrations or pre-migration backups.** Its bootstrap only ever
  *adds* — create missing tables, add missing columns, seed missing rows.
- **No interpretation of `_meta.schemaVersion`.** That is upstream's migration
  bookkeeping. A database written by a much newer Alyysha release should be
  checked before you rely on it.

## Backup

Take a backup first if the database matters:

```bash
cp -r ~/.alyysha/db ~/alyysha-db-backup   # stop the daemon first
```

## Binary portability

One binary per target, built from pure Go source with `CGO_ENABLED=0`. Every
release target must still build — that is the only thing constraining a
dependency here, not its size.

---
title: Parity
order: 12
section: Under the Hood
---

# Parity

Both projects share the same database and schema. What differs is the runtime.

## Comparison

| | Alyysha Router | Alyysha (upstream) |
|---|---|---|
| Runtime | Single Go binary | Node.js + Next.js |
| Dashboard | Embedded Svelte 5 | React 19 |
| Database | SQLite (modernc, pure Go) | SQLite |
| Schema | Same, additive | Same |
| Docker | Single binary | Node |
| Port | `20128` | `20128` |

## Guaranteed identical

- Request/response schemas for `/v1/*` — Chat Completions, Messages, Embeddings,
  Audio, Models.
- Dashboard endpoints `/api/*` — Connections, Combos, ProxyPools, Settings,
  ProviderNodes, Usage, Auth.
- Combo expansion, model fallback, account rotation, and strike-breaker quota
  handling.
- SQLite database compatibility (`~/.alyysha/db/data.sqlite`).

Both projects share the same database and schema, so a connection you add in
either dashboard shows up in the other. The supported setup is one active writer.

## What version am I running?

```bash
alyysha version
```

The dashboard's changelog modal shows both `CHANGELOG.md` and any fragment merged
since the last tag, so what you read there is current even between releases.

## Different by design

- Internal implementation leverages Go's performance, strong typing, small
  memory footprint, and idiomatic concurrency — it is not a transliteration of
  JavaScript.
- SSE streams flow directly through `io.Reader`/`io.Writer`, never buffered
  whole.
- SQLite access: one read per page, never N+1.

## Go-only features

Some features exist only in Alyysha Router and do not collide with upstream:

- Per-custom-provider limits (RPM/TPM/concurrency/quota/expiry)
- Per-API-key rate limiting with a FIFO queue
- USD + token budgets with a request-start hard cutoff
- AES-GCM credential vault
- Argon2id API key hashing
- PII and injection guardrails
- Semantic response cache
- Prometheus metrics

## Further reading

- Remaining parity work: [`docs/PARITY-REMAINING.md`](https://github.com/ShiroSdk/alyysha-router/blob/main/docs/PARITY-REMAINING.md)
- Upstream: [decolua/alyysha](https://github.com/decolua/alyysha)

For the KeiRouter port specifically — what was taken, what was skipped, and what
was never attempted — see [Advanced Features](advanced-features.html).

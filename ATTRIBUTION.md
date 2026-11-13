# Attribution & Ownership — Alyysha Router

This document separates the three layers of ownership in this repository. It
exists as good-faith evidence of where every feature came from: inherited code
is credited, ported concepts name their source, and original work is claimed.

## 1. Inherited (MIT, unchanged)

The foundation of this codebase is **alyysha-go** by Luqmanul Hakim
([luqman-v1/alyysha-go](https://github.com/luqman-v1/alyysha-go)) — itself a
Go + Svelte port of **alyysha** by decolua
([decolua/alyysha](https://github.com/decolua/alyysha)). All of it is MIT
licensed; the copyright notices are retained verbatim in [LICENSE](LICENSE)
as that license requires.

This layer covers everything inherited at the fork point: the gateway core,
provider lanes, dashboard, CLI, and the serenhope-parity ports completed
before the fork (per-key governance, token saver, security log, notification
feed, dashboard Fase 4). File-by-file listing is not attempted — the fork
commit history and the archived changelog
([docs/changelog/alyysha-go-legacy.md](docs/changelog/alyysha-go-legacy.md))
are the record.

Contributors whose ported work this product still runs on, credited where it
appears:

- **arewedaks** ([arewedaks/alyysha-go](https://github.com/arewedaks/alyysha-go)) — provider catalog, resilience, prompt-injection ports (see `wiki/advanced-features.md` and the legacy changelog).
- **QuantumNous** ([QuantumNous/new-api](https://github.com/QuantumNous/new-api)) — one referenced port in the legacy changelog.

## 2. Ported & adapted (concepts from other sources, implementation rewritten for Go/Alyysha Router)

| Feature | Source | Location in Alyysha Router |
|---|---|---|
| SSRF dispatch guard + PII masking | OmniRoute (cheaper-inference) — © 2026 diegosouzapw | `internal/proxy/dispatch_guard.go`, `internal/pii/` |
| Free-tier catalog page | OmniRoute concept | `web/src/lib/freeTierCatalog.ts`, `web/src/components/FreeTierView.svelte` |
| Routing strategies: cache-optimized (rendezvous), auto (5-factor), quota-weighted, reset-aware | OmniRoute circuitBreaker/accountFallback concepts | `internal/routing/`, `internal/handlers/chat/strategy_omniroute.go` |
| 3-tier circuit breaker | OmniRoute concept + resilience upgrade | `internal/resilience/` |
| Headroom GCF tabular compression + output styles (terse/less-code) | headroomlabs-ai/headroom — ported via the OmniRoute integration | `internal/compression/gcf/` |
| Telegram webhook + team/tenant cost centers | OmniRoute webhooks/teams concepts | `internal/webhooks/telegram.go`, `internal/teams/` |
| Web executors (chatgpt-web, gemini-web DOM capture/browser lease, deepseek-web PoW, muse-web) | serenhope/alyysha patterns | `internal/proxy/executor/*_web*.go` |
| A2A protocol surface | Public A2A spec (Agent-to-Agent protocol) | `internal/a2a/` |
| Webhooks multi-target (Telegram/Discord/Slack/Generic) | OmniRoute webhooks concept | `internal/webhooks/` |
| MCP server | Public MCP spec (Anthropic) | `internal/mcpserver/` |

Each of these is a new Go implementation (the sources above are TypeScript);
only the concepts and behavior contracts were ported. The `port(serenhope):`
and `port(omniroute):` commit messages are kept in history deliberately as
per-feature provenance.

## 3. Original Alyysha Router work (copyright YannClipper / ShiroSdk)

- **FASE 12 feature set** (original design from gap analysis, no upstream source): quality scoring, latency-aware routing, prefix clustering, anti-repeat, conversation failover, maintenance window, traffic ramp, session hygiene, request replay, anomaly detection.
- **Free-tier live tracking** (`freeTierBudget` table, per-provider reset loop, `GET /api/free-tier/live`) — original design beyond the catalog concept.
- **Session vault** (AES-GCM sealed webexec sessions over the vault DEK) and the **web-executor session pool** (bounded slots, sliding-window rate limit, idle reclamation, health loop).
- **Config bundle** (`alyysha export-bundle`/`import-bundle`, `sxb1:` sealed format, idempotent import).
- **TLS stealth controller + mandatory alert system** (daily compliance alert, permanent dashboard banner, profile catalog).
- **Compliance audit log v2** UI + CSV export, **notification→webhook fan-out** choke point.
- **Full rebranding**: Alyysha Router identity (name, UI, iconography), module path `alyysha/proxy`, `ALYYSHA_*` env var namespace with the one-version `internal/envcompat` fallback, fresh CHANGELOG, wiki/skills rebrand.
- All Go implementations of every ported concept above, the test suites
  built around them (79 packages), the TASK documents, and all
  performance tuning.

## 4. Third-party licenses

Major dependencies carried by this distribution, each under its own license:

| Dependency | License | Used for |
|---|---|---|
| github.com/bogdanfinn/tls-client (+ fhttp, utls) | MIT | TLS-fingerprint (JA3/JA4) stealth transports |
| github.com/go-chi/chi/v5 | MIT | HTTP routing |
| modernc.org/sqlite | MIT (BSD-derived) | Pure-Go SQLite driver |
| github.com/go-co-op/gocron, spf13/viper, other go.sum entries | see go.sum | utilities |
| shadcn-svelte (+ bits-ui, layerchart) | MIT | Dashboard component layer: chart, table, tooltip, separator |

Node dependencies of the dashboard are documented in `web/package.json` and
carry their own licenses (MIT/Apache-2.0/ISC in the main).

---

Updates to this file are part of the Definition of Done for every new phase,
together with CHANGELOG.md.

---
title: Combos
order: 7
section: Using the Gateway
---

# Combos

A combo is a named chain of models the router walks when a request arrives. It is
the mechanism behind "never stop coding": the first candidate is your
subscription, the next is cheap, the last is free.

## Strategies

| Strategy ID | Behaviour |
|---|---|
| `fallback` | Try candidates in order until one succeeds |
| `round-robin` | Rotate across candidates |
| `sticky` | Hold the same candidate while it succeeds |
| `capacity` | Skip candidates that cannot serve the request's capabilities |
| `fusion` | Query a multi-model panel and combine its results |
| `cache-optimized` | Prefer candidates likely to retain prompt-cache locality |
| `auto` | Choose an order using the available routing signals |
| `quota-weighted` | Weight candidates using available quota information |
| `reset-aware` | Score remaining quota with assumed windows; rotate leading-score ties, not actual provider reset timestamps |
| `lkgp` | Stable ascending order by provider usage-row count over the prior hour |

Despite its name, `lkgp` is a usage-based ranking, not a last-success health
check: its query does not filter for successful responses. It is applied in
the Chat Completions combo path, not explicitly in the separate
Messages/Responses loop. `reset-aware` receives the same remaining-quota
fraction for both windows and assumed reset distances of 5 hours/7 days, not
provider reset timestamps. Other strategy effects depend on available signals;
none guarantees an upstream quota or uninterrupted streaming.

## Combo budgets and model families

A combo can carry `budgetLimitUsd`, `budgetPeriod` (`daily`, `weekly`, `monthly`,
or `total`). Set the limit to `0` to clear it. The global `budget.breach_mode`
(`reject` or `downgrade`) selects the exhausted-budget action. The
request-start gate can reject with `402` or restrict the combo to its cheapest
**priced** member, returning `X-Budget-Downgraded: combo:<name>`.
An unpriced member is not a reliable cheapest choice.

This is not a universal spend reservation: in-flight requests can overshoot,
budget read failures fail open, and multi-member fusion bypasses this gate.
Per-key [governance](governance.html) is a separate control.

Family classification and the `crossFamily` result provide a derived warning
when members span model families. They do not enforce a strict family boundary
or automatically make different protocols interchangeable.

## Model arena

`POST /api/arena/run` accepts a prompt and 1–6 members. `maxTokens` defaults to
256 when outside the accepted 1–2048 range. Four workers share a 90-second
context. The reported latency measures arrival of response headers, not full
completion time.

The arena selects the first active connection, then checks its credentials.
If that connection is OAuth/cookie/unusable, it skips the member rather than
searching later connections for an API key. Accepted connections receive a direct
OpenAI-shaped request with Bearer authentication. It does not run the normal
router's proxy pools, custom headers, governance,
refresh protocols, or usage accounting. Use it for a limited comparison, not
as proof that the same members work through every client or combo path.

## Creating a combo

Dashboard → Combo & Vision Adapter → Create.

Two shortcuts build a chain for you:

- **Auto-free** (`POST /api/combos/auto-free`) — assembles a chain from the free
  tiers you have connected.
- **Auto-family** (`POST /api/combos/auto-family`) — builds one chain per model
  family, so `claude`, `gpt`, and `gemini` each get their own fallback.

**Presets** (`GET /api/combos/presets`) offer the Cursor and Claude default
lists. Every preset is *derived*, never hard-coded: the Claude items are whatever
the `cc` registry row currently holds plus the Claude Code aliases, so adding a
model to the provider registry adds a preset with no second edit.

## Smart combos

Smart combos keep a combo's membership in step with the models the gateway
actually knows about. Instead of listing model ids by hand, you write **rules**
and the router maintains the list.

### The two ideas a rule separates

| Notion | Meaning |
|---|---|
| **Addable** | A model a rule may put *into* the combo. Gated by reachability (a provider with a connection, a node that still exists) and by kind. |
| **Live** | A model that still *exists*. A member that is no longer live is pruned whatever the pattern says. |

A provider that loses its connection keeps its members — they are live, just
unreachable — but stops contributing new ones. Absence from a catalogue is weak
evidence of death, so a member is never pruned for being unreachable.

### Rule shape

| Field | Meaning |
|---|---|
| `pattern` | Glob, keyword, or regex (`re:` prefix required for regex) |
| `mode` | `glob` (default), `keyword`, `regex` |
| `effect` | `include` or `exclude` — an exclude rule always wins |
| `maxMembers` | How many models one rule may contribute. Default 20. `0` = unlimited, an explicit choice. |
| `allKinds` | Match non-LLM kinds too. Off by default: embedding, image, TTS and STT models cannot serve a chat turn. |
| `enabled` | Rule on/off |

Rules match against both spellings of a model — `alias/model` and bare `model` —
the same way the per-key allowlist does. A second qualified spelling (`AltID`)
means a member is never pruned for being spelled the other way round, and the
membership it adds still lands under one id, so a rule cannot create two slots
for one model.

### What survives a reconcile

- **Excluded ids** — a model you removed by hand is recorded in the rule's
  `excluded` list, so a later reconcile never re-adds it. A manual removal is
  permanent.
- **Managed members** — the picker never offers a model that is already a member,
  and the rules editor labels which members a rule owns.

**Known limitation:** a model id you pinned by hand *and* a rule claims is owned
by the rule, so editing the pattern removes it. The dashboard prevents the common
path, but the API cannot tell a pin from a claim when both spell the same id.

### Working with rules

```text
POST /api/combos/reconcile               Apply every combo's rules
POST /api/combos/{id}/smart/preview      Dry-run one combo — nothing is written
POST /api/combos/{id}/smart/load         Apply one combo's rules
```

A preview reports what would change before anything is committed:

```json
{
  "models":  ["ag/gemini-3-flash", "oc/free-model"],
  "added":   ["oc/free-model"],
  "removed": [],
  "rules":   [{ "id": "r1", "matched": 41, "applied": 20, "capped": true }]
}
```

`matched` is what the pattern selected; `applied` is what actually landed after
the cap; `capped` tells you the cap bit.

## Timeout and grace period

Set **Model response timeout** per combo in the Edit dialog (seconds, decimals
such as `5.3` are allowed; empty or `0` disables), or from the compact field on
the combo card. The timer covers each model candidate except the last,
**including connection selection and waiting for a usable response**. The last
candidate keeps the gateway's normal deadline, so the setting never makes a
combo fail sooner than it would without it (#744). An active stream is not cut
off by this setting; for non-streaming requests the timer covers the wait for
the complete response.

When it fires, the candidate is cancelled and the next is tried; the timed-out
model is then left out of that combo for a grace period — default 5 minutes,
set per combo under the timeout in the Edit dialog, `0` re-includes it on the
next request — before rejoining the chain. Fusion panel timing is separate: a
model that times out there is benched by the same grace logic, and if every
model in the chain is inside its grace period, the full chain is used. The
pause is held in memory and resets on restart.

**Where the value is stored:** the dashboard shows seconds; the setting is
stored in milliseconds as `comboStrategies[comboName].firstTokenTimeoutMs`,
with the grace period next to it as `firstTokenGraceMs`.

## Where it lives

- `internal/handlers/chat/combo.go` — dispatch, failover, and the two combo loops
- `internal/combos/smart/smart.go` — the rule resolver (pure, no I/O)
- `internal/combopresets/presets.go` — the derived Cursor/Claude presets
- `web/src/components/combos/` — the dashboard UI

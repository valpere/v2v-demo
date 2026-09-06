You are doing a **dreaming pass** for the **v2v-demo** project only —
async, scheduled curation of this project's context. Sleep-time
consolidation: review what accumulated, identify patterns, suggest curation.
Read-only: produce a report, change nothing. Do NOT look at other projects.

## Project context

- **v2v-demo** — a Telegram voice assistant demo for a translation bureau
  (neutral scenario, fabricated data). Shipped as a client demo (v0.1.0),
  now confirmed to be a base for ongoing development — features land
  deliberately post-release, config-gated and opt-in by default.
- Source of truth: `AGENTS.md` (conventions, non-goals) + `.agents/plan.md`
  (file-by-file how, authoritative for package layout) + `docs/requirements.md`
  (FR-/NFR-/D- IDs) + `docs/architecture.md` (shape, authoritative).
  `.agents/changes.md` records what shipped past v0.1.0.
- Backend switches are config-driven: `STT_BACKEND` (local/openai),
  `DIALOG_BACKEND` (ollama/openai/gemini), `TTS_BACKEND` (elevenlabs/azure),
  `SESSION_STORE` (memory/sqlite, opt-in).
- Non-goals (explicit): real Zoho / telephony / vector RAG / multi-tenant /
  auth. `Session` persistence is scoped to session data only, not a
  general-purpose datastore.
- Git: direct-to-main, no branch protection (private repo).

## Targets

| Path | What to look for |
|------|------------------|
| `AGENTS.md` | Drift between stated conventions and actual code (gofmt/vet clean, one-dependency-per-concern, no speculative config surface) |
| `.agents/plan.md` | Package-layout invariant — has the actual `internal/`/`cmd/`/`bot/` structure drifted from what the plan documents without an accompanying plan update? |
| `.agents/changes.md` | Is it actually being updated as features land post-v0.1.0, or has drift crept in unrecorded? |
| `docs/requirements.md` | FR-/NFR-/D- trace gaps — any implemented behavior without a traced requirement, or vice versa |
| `docs/architecture.md` | Still matches the real data flow / component shape, or has an unrecorded shape change happened |
| `~/.claude/projects/-home-val-wrk-demo-v2v-demo-v2v-demo/memory/` | Stale memory files vs current code (backend defaults, model IDs) |
| `AGENTS.md` "Non-goals" | Scope creep — has `SESSION_STORE` or anything else quietly grown beyond its stated scope? |
| `git log --since="2 weeks ago"` | Recurring failure patterns, oft-reverted commits |
| `tmp/` vs `minions/` | Any script sitting in `tmp/` long enough it should have been promoted to `minions/` per `AGENTS.md`'s own stated convention |

## What to find

### 1. Package-layout / plan drift (highest priority)
`.agents/plan.md` is authoritative for structure. Compare its file-by-file
section against the real tree. Any restructuring done without updating the
plan in the same commit is a should-fix finding — flag the specific
mismatch.

### 2. Non-goals scope creep
Walk `AGENTS.md`'s "Non-goals" list. For `SESSION_STORE=sqlite`
specifically: confirm it's still scoped to `Session` data only, not
growing into a general-purpose datastore. Flag any new schema/table that
isn't `Session`-shaped.

### 3. Config-gating discipline
New features should land "config-gated and opt-in by default" per
`AGENTS.md` line 1. Check recent commits for anything that changed a
default behavior without a corresponding env var / opt-in gate.

### 4. Requirements/architecture trace
FR-/NFR-/D- IDs in `docs/requirements.md` vs what's actually wired in
`dialog.Handle`'s behavioural spec section of `.agents/plan.md`. Flag
drift either direction.

### 5. Memory & doc staleness
Memory files vs current code — backend defaults (`STT_BACKEND`,
`DIALOG_BACKEND`, `TTS_BACKEND`), model IDs (`gemma4:cloud` etc.) vs what's
actually configured now.

### 6. Recurring commit themes
`git log --since="2 weeks ago" --oneline`. Patterns that recur → candidates
for a new AGENTS.md rule or a test.

## Method

1. **Read** `AGENTS.md` first — conventions and non-goals.
2. **Read** `.agents/plan.md`'s structure section and compare against the
   real tree (`find . -type d`).
3. **Sample** recent commits and `.agents/changes.md`.
4. **Read** memory selectively by mtime; cross-compare against `AGENTS.md`.
5. **Check** `docs/requirements.md` trace against `.agents/plan.md`.
6. Output a concise report grouped by severity (blocker / should-fix / nit),
   each finding with a concrete path and a suggested action. Suggest only;
   change nothing.

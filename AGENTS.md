# AGENTS.md — v2v-demo

A Telegram voice assistant for a translation bureau, neutral scenario,
fabricated data. Shipped as a client demo (v0.1.0); **confirmed 2026-09-05
to now be a base for ongoing development, not throwaway** — features land
deliberately post-release, each still config-gated and opt-in by default
(see `.agents/changes.md` for what's shipped past v0.1.0). Still **not
production, not a framework**. See `.agents/plan.md` for the original spec.

## Conventions

- Go 1.26+. Standard library first; at most one dependency per concern, and
  only when it clearly beats hand-rolling.
- `gofmt` / `go vet` clean. `go build ./...` and `go test ./... -race` must
  pass before any commit.
- Package layout is fixed by `.agents/plan.md` — do not restructure it
  without updating that doc in the same commit.
- No secrets in code or logs. Config comes from env (`.env.example`).
- Keep it simple. A new feature still needs a clear, explicit ask (or an
  approved plan) — don't add abstraction or config surface speculatively.
- `tmp/` — throwaway scratch (gitignored). `minions/` — dev helper scripts
  that get reused (tracked; Bash, not Python). Promote a script from `tmp/` to
  `minions/` once it's clearly reusable.

## Non-goals

Real Zoho / telephony / vector RAG / multi-tenant / auth. Session
persistence is now in scope, opt-in (`SESSION_STORE=sqlite`, see
`docs/requirements.md` §1 `@schema Config`) — it is not a general-purpose
datastore for anything beyond `Session`. If the plan and a request
conflict, follow the plan and note it in `.agents/changes.md`.

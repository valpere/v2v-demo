# AGENTS.md — v2v-demo

Throwaway client demo. A Telegram voice assistant for a translation bureau,
neutral scenario, fabricated data. **Not production, not a framework.** See
`.agents/plan.md` for the full spec.

## Conventions

- Go 1.26+. Standard library first; at most one dependency per concern, and
  only when it clearly beats hand-rolling.
- `gofmt` / `go vet` clean. `go build ./...` and `go test ./... -race` must
  pass before any commit.
- Package layout is fixed by `.agents/plan.md` — do not restructure it.
- No secrets in code or logs. Config comes from env (`.env.example`).
- Keep it simple. This code is thrown away after the client decides. Do not
  add abstraction, config surface, or features beyond the plan.
- `tmp/` — throwaway scratch (gitignored). `minions/` — dev helper scripts
  that get reused (tracked; Bash, not Python). Promote a script from `tmp/` to
  `minions/` once it's clearly reusable.

## Non-goals

Real Zoho / telephony / vector RAG / multi-tenant / persistence / auth. If the
plan and a request conflict, follow the plan and note it in
`.agents/changes.md`.

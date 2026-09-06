---
name: apply-dreaming
description: "Read the latest v2v-demo dreaming report and apply
  high-confidence findings. Repo is direct-to-main (no branch
  protection) — commits land straight to main. Direct edits for
  project memory. Annotates report with [applied YYYY-MM-DD] markers.
  Usage: /apply-dreaming [week|latest]"
user-invocable: true
argument-hint: "[week|latest]"
---

# /apply-dreaming (v2v-demo)

Weekly review skill for processing v2v-demo dreaming reports from
`.claude/dreaming/reports/`. Walks items interactively.

## When to invoke

- Monday morning after Sunday night's dreaming run.
- Or after manual `.claude/dreaming/dreaming.sh`.
- Or `/apply-dreaming [latest|YYYY-W##]`.

## Steps

### 1. Locate report

```bash
WEEK="${1:-latest}"
DIR=".claude/dreaming/reports"
if [[ "$WEEK" == "latest" ]]; then
  REPORT=$(ls -1t "$DIR"/2026-W*.md 2>/dev/null | head -1)
else
  REPORT="$DIR/$WEEK.md"
fi
```

### 2. Triage walk

Iterate `high → medium → low`. Per item:
- `[a]pply / [s]kip / [v]erify-first / [e]vidence / [q]uit`

For `low`: skip silently unless user opted in. **Non-goals scope-creep
findings are always high priority regardless of the report's own
confidence label** — `SESSION_STORE` or any new feature growing beyond
its documented scope is a should-fix at minimum, never silently skip.

### 3. Apply per category

#### `update-memory` — `~/.claude/projects/.../memory/<file>.md`

Local-only, gitignored. Edit directly. No commit needed.

#### `update-rules` / `update-plan` / `update-requirements`

1. Edit `AGENTS.md`, `.agents/plan.md`, or `docs/requirements.md`
   directly on `main` (this repo is direct-to-main, no branch
   protection).
2. Commit, push.

#### `code-change` / `add-test`

1. Implement the change directly on `main`.
2. `go build ./... && go vet ./... && go test ./... -race`.
3. Commit, push.
4. `/fix-review` on the diff for quality, if installed.

#### `forget-memory`

1. Backup to `./tmp/dreaming-W##-v2v-demo-backup-HHMM/` (this
   project's own scratch dir — the `block-system-tmp.sh` hook wired
   into this project's `.claude/settings.local.json` denies writes
   into system `/tmp`).
2. Confirm, then `rm <file>`.

### 4. Annotate report

```markdown
> [applied YYYY-MM-DD: <action>; commit <sha>]
```

## Constraints

- **Direct-to-main is correct here** — no branch protection on this repo,
  don't invent a PR step.
- **Never auto-apply low confidence** without explicit request, except
  non-goals scope-creep findings (always surface, never silently skip).
- **Always run `go build ./... && go test ./... -race`** after code changes.
- **Always cite report section** in commit messages.

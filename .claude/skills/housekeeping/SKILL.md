---
name: housekeeping
description: "Recurring repo health check for v2v-demo (Go / Telegram bot). Universal hygiene + Go (vet, gofmt) + topics-manifest integrity. Pass/fail table. Usage: /housekeeping"
---

# Skill: /housekeeping
# Repo Health Check — v2v-demo

---

## OVERVIEW

```
/housekeeping  →  run checks  →  Markdown table: Check | Status | Detail
                              →  Summary: N passed, M failed
```

Read-only. Never modifies files, never commits, never opens a PR.
Run any time for a hygiene snapshot. Any FAIL = exit signal to fix before shipping.

Generated from `~/wrk/common/skills/housekeeping/` for this Go project
(2026-09-08). Re-run `/generate-housekeeping` to regenerate if the stack changes.

---

## UNIVERSAL CHECKS

### Check 1 — Stale Local Branches

**Goal:** ≤ 10 local branches after pruning remote-tracking refs.

```bash
git remote prune origin 2>&1 | tail -3
LOCAL_COUNT=$(git branch | grep -v '^\*' | wc -l | tr -d ' ')
```

**Pass:** `LOCAL_COUNT <= 10`
**Fail:** "N local branches — prune merged ones"

Cleanup tip:
```bash
git branch --merged main | grep -v 'main\|^\*'
# delete with: git branch -d <branch>
```

---

### Check 2 — Debug Output in Source

**Goal:** Zero stray `fmt.Print*` in production Go source (excluding `*_test.go`
and the `minions/` dev helpers, which are CLI tools that print by design).

```bash
COUNT=$(grep -rl --include="*.go" --exclude="*_test.go" \
  "fmt\.Println\|fmt\.Printf\|fmt\.Print(" cmd/ internal/ 2>/dev/null \
  | wc -l | tr -d ' ')
```

**Pass:** `COUNT == 0` (the bot logs via `log.*`, never `fmt.Print*`)
**Fail:** list offending files (up to 5, then "+ N more")

---

### Check 3 — Tracked Secret .env File

**Goal:** the live `.env` (and any `.env.*` that isn't a committed template)
must not be tracked. `.env.example` and `.env.client` are intentional
templates — `.gitignore` un-ignores exactly those two.

```bash
TRACKED=$(git ls-files '.env' '.env.*' '*.env' 2>/dev/null \
  | grep -vE '\.env\.(example|client)$')
```

**Pass:** empty result
**Fail:** "`<file>` is tracked — add to .gitignore and run `git rm --cached <file>`"

---

### Check 4 — Tracked Backup / Scratch Files

**Goal:** `backup/`, `tmp/`, `data/*.jsonl`, `*.bak` must not be tracked.

```bash
TRACKED=$(git ls-files backup/ tmp/ 'data/*.jsonl' '*.bak' '.env.dev*' 2>/dev/null)
```

**Pass:** empty result
**Fail:** list the tracked files

---

### Check 5 — TODO/FIXME Count (informational)

**Goal:** Report count. No threshold — visibility only.

```bash
COUNT=$(grep -r --include="*.go" \
  -E "//\s*(TODO|FIXME)" \
  --exclude-dir=.git . 2>/dev/null | wc -l | tr -d ' ')
```

**Status:** Always `INFO`.
**Detail:** "N TODO/FIXME comments" — append " (consider a cleanup sprint)" if > 20.

This check never contributes to the failed count.

---

### Check 6 — Project Layout Drift

**Goal:** files in `docs/` should not be working drafts or duplicates of
canonical artifacts in the sibling `../context/` directory.

```bash
STRAY=$(find docs -maxdepth 1 -type f \( \
  -name '*-DRAFT.md' -o \
  -name 'review-prompt-*.md' -o \
  -name '*-iter[0-9]*.md' -o \
  -name '20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-*.md' \
\) 2>/dev/null)

DUPES=""
if [ -d ../context ]; then
  for f in docs/*.md; do
    [ -f "$f" ] || continue
    base=$(basename "$f")
    [ -f "../context/$base" ] && DUPES="$DUPES $f (exact: ../context/$base)" && continue
    for ctx in ../context/20[0-9][0-9]-[0-9][0-9]-[0-9][0-9]-"$base"; do
      [ -f "$ctx" ] && DUPES="$DUPES $f (date-prefixed twin: $ctx)" && break
    done
  done
fi
```

**Pass:** no strays, no duplicates.
**Fail:** list offenders (up to 5, then "+ N more"). Files may be intentional
repo-distribution copies, or strays to delete / move to `../context/`.
**Skip:** `../context/` does not exist. (v2v-demo lives under `~/wrk/demo/`,
not `~/wrk/projects/`, so this normally SKIPs.)

---

### Check 7 — CLAUDE.md Key Files Exist

**Goal:** files listed under a "Key Files" / "Ключові файли" section of
`CLAUDE.md` should exist on disk.

```bash
MISSING=""
if [ -f CLAUDE.md ]; then
  MISSING=$(awk '
    /^##.*[Kk]ey [Ff]iles|^##.*[Кк]лючов.*файл/ {in_section=1; next}
    /^##/ && in_section {in_section=0}
    in_section
  ' CLAUDE.md | grep -oE '`[^`]+`' | tr -d '`' | while read f; do
    case "$f" in
      */*|*.md|*.go|*.py|*.ts|*.tsx|*.js|*.yaml|*.yml|*.json|*.toml)
        [ -e "$f" ] || echo "$f" ;;
    esac
  done)
fi
```

**Pass:** all listed files exist (or no Key Files section).
**Fail:** list missing paths — likely doc drift after a rename/delete.
**Skip:** no `CLAUDE.md` at repo root.

---

### Check 8 — Skill Temp Dir Accumulation (informational)

```bash
COUNT=$(find ~ /tmp -maxdepth 2 -type d -mtime +7 \
  \( -name 'fix-review-*' -o -name 'lookup-docs-*' -o -name 'apply-dreaming-*' -o -name 'council' \) \
  2>/dev/null | wc -l | tr -d ' ')
```

**Status:** Always `INFO`.
**Detail:** "N stale skill/tool temp dirs" — append cleanup hint if > 5.

Never contributes to the failed count.

---

## STACK-SPECIFIC CHECKS (Go)

### Check 9 — go vet

**Goal:** `go vet ./...` passes with zero errors.

```bash
VET=$(go vet ./... 2>&1)
```

**Pass:** no output (exit 0).
**Fail:** first 10 lines of the vet output.
**Skip:** `go` not on PATH.

---

### Check 10 — Formatting (gofmt)

**Goal:** every tracked `.go` file is gofmt-clean (mirrors `make fmt-check`).

```bash
UNFMT=$(gofmt -l . 2>/dev/null)
```

**Pass:** empty (0 files).
**Fail:** list the unformatted files (up to 5, then "+ N more").
**Skip:** `gofmt` not on PATH.

---

## PROJECT-SPECIFIC CHECKS

### Check 11 — Topics Manifest Integrity

**Goal:** `topics/topics.json` is valid, and every `kb` / `system_prompt` /
`greeting` path it names exists — a rename that misses the manifest breaks the
bot at startup (`loadTopics` fails hard).

```bash
python3 - <<'PY'
import json, os, sys
try:
    d = json.load(open("topics/topics.json"))
except Exception as e:
    print(f"INVALID JSON: {e}"); sys.exit(1)
bad = []
seen = set()
for t in d:
    tid = t.get("id", "?")
    if tid in seen: bad.append(f"duplicate id {tid!r}")
    seen.add(tid)
    if not (t.get("scope_uk") and t.get("scope_en")): bad.append(f"{tid}: missing scope_uk/scope_en")
    if not t.get("slots"): bad.append(f"{tid}: no slots")
    for key in ("kb", "system_prompt", "greeting"):
        p = t.get(key)
        if not p: bad.append(f"{tid}: no {key}"); continue
        if not os.path.isfile(p): bad.append(f"{tid}: {key} → {p} (missing)")
if bad:
    print("\n".join(bad)); sys.exit(1)
print(f"OK — {len(d)} topics, all paths resolve")
PY
```

**Pass:** exit 0 ("OK — N topics, all paths resolve").
**Fail:** list the problems (missing file, bad JSON, no slots, dup id).
**Skip:** `python3` not on PATH, or `topics/topics.json` absent.

---

## OUTPUT FORMAT

```
## /housekeeping — Repo Health Report (v2v-demo)

| Check | Status | Detail |
|-------|--------|--------|
| Stale local branches | PASS | 1 local branch |
| Debug output in src | PASS | — |
| Tracked .env | PASS | — |
| Tracked backup/scratch | PASS | — |
| TODO/FIXME count | INFO | 3 TODO/FIXME comments |
| Project layout drift | SKIP | no ../context/ |
| CLAUDE.md key files | SKIP | no CLAUDE.md |
| Skill temp dirs | INFO | 2 stale skill/tool temp dirs |
| go vet | PASS | — |
| gofmt | PASS | — |
| Topics manifest integrity | PASS | 5 topics, all paths resolve |

**7 passed, 0 failed** (2 informational, 2 skipped)
```

Status values: `PASS` / `FAIL` (must fix) / `INFO` (never a failure) / `SKIP` (not a failure).

Summary: `N passed, M failed` — with optional `(K informational, J skipped)`.

---

## RULES

1. **Read-only** — never modify files, commit, push, or open a PR.
2. **Run from repo root** — all paths relative to repository root.
3. **INFO checks never count as failures** (TODO/FIXME, skill temp dirs).
4. **SKIP is not failure.**
5. **Graceful degradation** — tool unavailable → mark SKIP and continue.
6. **No auto-fix** — this skill reports. For fixes: `make fmt`, `/code-review`,
   the relevant topic edit.
7. **Exit signal** — if any check is FAIL, end with:
   "Run /housekeeping again after fixing the issues above."

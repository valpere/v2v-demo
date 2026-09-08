---
name: generate-housekeeping
description: "Generates a project-specific /housekeeping skill with stack-aware checks (framework versions, CI coverage delta, lint). Run once per project. Usage: /generate-housekeeping"
---

# Skill: /generate-housekeeping
# Generate Project-Specific /housekeeping Skill

Analyzes the current project and writes a customized `/housekeeping` skill to
`.claude/skills/housekeeping/SKILL.md` with additional stack-specific checks.

---

## DISCOVERY STEPS

### Step 1 — Detect stack and framework versions

```bash
# JavaScript/TypeScript
cat package.json 2>/dev/null | python3 -c "
import sys,json; d=json.load(sys.stdin)
deps = {**d.get('dependencies',{}), **d.get('devDependencies',{})}
for k in ['react','vite','typescript','tailwindcss','@supabase/supabase-js']:
    if k in deps: print(f'{k}: {deps[k]}')
" 2>/dev/null

# Go
cat go.mod 2>/dev/null | head -10

# Current versions used in docs/agents (to detect drift)
grep -r --include="*.md" -oh "React [0-9][0-9]*\|Vite [0-9]\|TypeScript [0-9]\|Go [0-9]\.[0-9]*" \
  .claude/ docs/ CLAUDE.md 2>/dev/null | sort | uniq -c | sort -rn | head -10
```

### Step 2 — Find CI workflow and coverage config

```bash
ls .github/workflows/ 2>/dev/null
# Does CI upload coverage artifacts?
grep -l "coverage\|upload-artifact" .github/workflows/*.yml 2>/dev/null | head -3
# Vitest coverage config
grep -A5 "coverage" vitest.config.ts vite.config.ts 2>/dev/null | head -10
```

### Step 3 — Detect issue tracker

```bash
grep -r "gh issue\|gh pr" .claude/ --include="*.md" -l 2>/dev/null | head -3
```

### Step 4 — Find doc version references

```bash
# Find all framework version mentions in docs and agents
grep -r --include="*.md" \
  -oE "React [0-9]+|Vite [0-9]+|TypeScript [0-9]+|Go [0-9]+\.[0-9]+" \
  .claude/ docs/ CLAUDE.md 2>/dev/null | sort | uniq -c | sort -rn | head -15
```

### Step 5 — Check for existing stack-specific gotchas

```bash
cat CLAUDE.md 2>/dev/null | head -80
```

---

## OUTPUT

Write to `.claude/skills/housekeeping/SKILL.md`.

Keep the 5 universal checks intact. Add a **Stack-Specific Checks** section after Check 5:

**For React/Vite/TypeScript projects**, add:

```
### Check 6 — Framework Version Drift in Docs

Goal: No docs/agent files reference an older major version of a core framework.

Detect current React version from package.json. Scan .claude/ + docs/ + CLAUDE.md
for mentions of "React {old_version}" where old_version != current.

Pass: no stale version mentions
Fail: list files containing the stale version reference
```

```
### Check 7 — CI Coverage Delta

Goal: Coverage hasn't regressed since last successful CI run.

[Detect project's CI coverage tool: gh run list --workflow=<ci-workflow>.yml,
download artifact, compare total.lines.pct between last two successful main runs]
```

**For Go projects**, add:

```
### Check 6 — go vet

Goal: go vet must pass with zero errors.

Run: go vet ./... 2>&1

Pass: no output (exit 0)
Fail: list the vet errors
```

```
### Check 7 — Formatting (gofmt/goimports)

Goal: All .go files are properly formatted.

Run: gofmt -l . | grep -v vendor | wc -l

Pass: 0 unformatted files
Fail: list unformatted files
```

**For Python projects**, add:

```
### Check 6 — Linting (ruff/flake8)

Goal: No lint errors in production source.

Run: ruff check . --exclude tests/ 2>/dev/null || flake8 --exclude tests/ 2>/dev/null

Pass: no errors
Fail: first 10 errors
```

---

## RULES

- Write to `.claude/skills/housekeeping/SKILL.md` — overwrite generic if present.
- Keep all 5 universal checks intact; only add to them.
- Number added checks starting from 6.
- Update the frontmatter description to include the project name.
- After writing, confirm: "Wrote project-specific /housekeeping. N total checks."

---
name: session-end
description: "Manages .claude/session-log.md — a per-project rolling log of session summaries, one entry per day, last 10 kept. Usage: /session-end | /session-end show | /session-end all"
---

# /session-end

Manages `.claude/session-log.md` — a per-project rolling log of session
summaries, one entry per day, last 10 entries kept (count-based, not age-based).

```
/session-end          → write today's summary (manual, high quality)
/session-end show     → print the last entry
/session-end all      → print all entries (oldest → newest)
```

---

## /session-end (no args) — write today's summary

Review the current conversation and write today's entry to
`.claude/session-log.md`. Use this before switching projects or closing
Claude Code. The Stop hook auto-generates a summary on exit, but
`/session-end` produces better output — full session context, not
transcript extraction.

If an entry for today already exists it is **replaced**, not duplicated.
After write, the log is rotated to keep the last 10 entries.

### Entry format

```markdown
## YYYY-MM-DD

### Що зробили
- completed item

### Поточний стан
- current branch / open PR number
- what is working / what is broken

### Відкриті питання
- unresolved question (omit section if none)

### Наступні кроки
- next item, in priority order
```

Rules: 10–20 bullets total · Ukrainian for content · English for code/files/identifiers · include PR number and branch in Поточний стан · omit Відкриті питання if none.

### Write logic

```python
import re, os
from datetime import date

log = '.claude/session-log.md'
today = str(date.today())
max_keep = 10

entry = """## {today}

### Що зробили
- ...

### Поточний стан
- ...

### Наступні кроки
- ...""".format(today=today).strip()

# fill in entry content based on current session, then:

if not os.path.exists(log):
    open(log, 'w').write(entry + '\n')
else:
    content = open(log).read()
    parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', content)
    entries = [p.strip() for p in parts if p.strip()]
    today_idx = next((i for i, e in enumerate(entries) if e.startswith(f'## {today}')), -1)
    if today_idx >= 0:
        entries.pop(today_idx)
        entries.append(entry)        # replace today, moved to end
    else:
        entries.append(entry)        # new day
    entries = entries[-max_keep:]    # freshest write can never be trimmed away
    open(log, 'w').write('\n\n'.join(entries) + '\n')
```

After writing, report:
> "Session log updated — `.claude/session-log.md`, entry for {today}."

---

## /session-end show — print last entry

```python
import re

try:
    with open('.claude/session-log.md') as f:
        parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', f.read())
    entries = [p.strip() for p in parts if p.strip()]
    print(entries[-1] if entries else '(no entries)')
except FileNotFoundError:
    print('No session log found. Run `/session-end` to create one.')
```

---

## /session-end all — print all entries

```python
import re

try:
    with open('.claude/session-log.md') as f:
        parts = re.split(r'(?m)(?=^## \d{4}-\d{2}-\d{2})', f.read())
    entries = [p.strip() for p in parts if p.strip()]
    for e in entries:
        print(e)
        print()
    if entries:
        dates = [e.split('\n')[0].replace('## ', '') for e in entries]
        print(f"{len(entries)} entries: {dates[0]} → {dates[-1]}")
except FileNotFoundError:
    print('No session log found.')
```

---

## How the full system works

```
Session ends (exit / /exit)
  ├─ Stop hook: .claude/hooks/session-end.sh
  │    ├─ Skip if /session-end ran within 2h (file mtime check)
  │    ├─ LLM fallback chain (this project):
  │    │    1. agy -p        — Gemini 3.7/3.6/3.5 Flash (cheapest first)
  │    │    2. opencode run --format json — ollama/*:cloud
  │    │       (deepseek-v4-flash, glm-5.3-flash, minimax-m3, qwen3-coder-next, deepseek-v4-pro)
  │    │    3. Raw transcript excerpt (fallback)
  │    └─ Rotates session-log.md to last 10 entries (count-based)
  └─ Stop hook: .claude/hooks/session-index.sh
       └─ session-indexer mine — JSONL transcript → .claude/sessions.db (append-only)

Next session opens
  ├─ SessionStart hook: .claude/hooks/session-last.sh
  │    └─ Injects last ## YYYY-MM-DD entry from session-log.md
  └─ SessionStart hook: .claude/hooks/session-recall.sh
       └─ Semantic search over .claude/sessions.db by branch+commit context
```

The skill works standalone (without hooks) — run `/session-end` manually.

**Log file**: `.claude/session-log.md` — gitignored.
**Session index**: `.claude/sessions.db` — gitignored; query with `/recall <query>` (user-level skill).

**Troubleshooting**:
```bash
tail -30 ~/.cache/v2v-demo/hooks.log
cat .claude/session-log.md
session-indexer stats --db .claude/sessions.db
```

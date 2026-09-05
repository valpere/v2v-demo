#!/usr/bin/env bash
# minions/council/run.sh — fan a code-review prompt out to several independent
# third-party coding-agent CLIs in parallel and collect their reports.
#
# Every agent is invoked in a read-only / plan mode where the CLI offers one,
# plus an explicit "do not edit anything" instruction in the shared prompt as
# a second line of defense. This is best-effort, not a sandbox guarantee —
# check `git status` after a run.
#
# Usage:
#   minions/council/run.sh [-r RANGE] [-o OUTDIR] [-a agent1,agent2,...] [-t SECS] [-b BRIEF_FILE]
#
#   -r RANGE       git range to review, e.g. 31f3e75..HEAD (default: HEAD~3..HEAD)
#   -o OUTDIR      where reports go (default: tmp/council/<UTC timestamp>)
#   -a AGENTS      comma-separated subset of the agents below (default: all configured)
#   -t SECS        per-agent timeout (default: 900)
#   -b BRIEF_FILE  extra review instructions appended to the shared prompt (optional)
#
# Agents (see agent_<name> functions below to add/remove/reconfigure one):
#   opencode      opencode/nemotron-3-ultra-free, --agent reviewer
#   cursor-agent  --model auto, --mode plan
#   kiro-cli      --trust-tools=fs_read (no write/exec tools trusted)
#   kilo          kilo-auto/free, --agent plan
#   codex         (needs --dangerously-bypass-approvals-and-sandbox in this
#                 sandboxed shell — codex's own bubblewrap sandbox can't nest.
#                 Off by default: pass -a codex explicitly once your ChatGPT
#                 Codex quota has room; check `codex exec "hi"`.)
#   omp           (off by default: this account's OpenRouter credit was too
#                 low to run a real review as of 2026-09-05. `omp --login`
#                 or top up, then pass -a omp explicitly.)
#
# Output: OUTDIR/<agent>.md (stdout), OUTDIR/<agent>.stderr.log, OUTDIR/<agent>.exit

set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

RANGE="HEAD~3..HEAD"
OUTDIR=""
AGENTS="opencode,cursor-agent,kiro-cli,kilo"
TIMEOUT=900
BRIEF_FILE=""

while getopts "r:o:a:t:b:h" opt; do
	case "$opt" in
	r) RANGE="$OPTARG" ;;
	o) OUTDIR="$OPTARG" ;;
	a) AGENTS="$OPTARG" ;;
	t) TIMEOUT="$OPTARG" ;;
	b) BRIEF_FILE="$OPTARG" ;;
	h)
		sed -n '2,30p' "$0"
		exit 0
		;;
	*)
		echo "usage: $0 [-r RANGE] [-o OUTDIR] [-a AGENTS] [-t SECS] [-b BRIEF_FILE]" >&2
		exit 2
		;;
	esac
done

[ -z "$OUTDIR" ] && OUTDIR="tmp/council/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$OUTDIR"

# The diff is embedded directly rather than left for each agent to fetch via
# `git diff` — some of these CLIs are run with no shell/exec tool trusted
# (kiro-cli --trust-tools=fs_read), so they'd have no way to get it themselves.
PROMPT_FILE="$OUTDIR/prompt.md"
{
	echo "You are an independent code reviewer. Below is a git commit log and"
	echo "unified diff for range $RANGE of a Go repository (a Telegram voice-"
	echo "assistant bot). The working directory, if you have file-read tools, is"
	echo "the root of this same repository — read any file the diff references"
	echo "for more context (docs/requirements.md, docs/architecture.md,"
	echo ".agents/plan.md, the full content of a changed source file, etc.)."
	echo
	echo "Focus on: correctness, concurrency/race conditions, consistency between"
	echo "the code and its docs, missing test coverage, and regressions."
	if [ -n "$BRIEF_FILE" ]; then
		echo
		cat "$BRIEF_FILE"
	fi
	echo
	echo "Reply with a markdown report: a numbered list of findings, each with"
	echo "file:line, a one-sentence description, and a severity"
	echo "(critical/major/minor/nit). If you find nothing, say so plainly."
	echo
	echo "IMPORTANT: this is a read-only review. Do not edit, write, or commit"
	echo "any file — only read and analyze. Your final chat message IS the report."
	echo
	echo '```'
	echo "commit log:"
	git log --oneline "$RANGE"
	echo
	echo "diff:"
	git diff "$RANGE"
	echo '```'
} >"$PROMPT_FILE"

agent_opencode() { opencode run --agent reviewer --model opencode/nemotron-3-ultra-free "$(cat "$PROMPT_FILE")"; }
agent_kilo() { kilo run --agent plan --model kilo/kilo-auto/free "$(cat "$PROMPT_FILE")"; }
agent_cursor_agent() { cursor-agent --print --mode plan --model auto "$(cat "$PROMPT_FILE")"; }
agent_kiro_cli() { kiro-cli chat --no-interactive --trust-tools=fs_read "$(cat "$PROMPT_FILE")"; }
agent_codex() { codex --dangerously-bypass-approvals-and-sandbox exec "$(cat "$PROMPT_FILE")"; }
agent_omp() { omp --print --model auto "$(cat "$PROMPT_FILE")"; }

export PROMPT_FILE
echo "council: range=$RANGE agents=$AGENTS timeout=${TIMEOUT}s out=$OUTDIR"

IFS=',' read -ra LIST <<<"$AGENTS"
pids=()
names=()
for name in "${LIST[@]}"; do
	fn="agent_${name//-/_}"
	if ! declare -F "$fn" >/dev/null; then
		echo "council: unknown agent '$name' (no $fn function) — skipping" >&2
		continue
	fi
	export -f "$fn"
	(
		set +e
		timeout "$TIMEOUT" bash -c "$fn" </dev/null \
			>"$OUTDIR/$name.md" 2>"$OUTDIR/$name.stderr.log"
		echo $? >"$OUTDIR/$name.exit"
	) &
	pids+=($!)
	names+=("$name")
	echo "council: launched $name (pid $!)"
done

for pid in "${pids[@]}"; do
	wait "$pid" || true
done

echo
printf '%-14s %-6s %-8s %s\n' "AGENT" "EXIT" "LINES" "REPORT"
for name in "${names[@]}"; do
	exitcode=$(cat "$OUTDIR/$name.exit" 2>/dev/null || echo "?")
	lines=$(wc -l <"$OUTDIR/$name.md" 2>/dev/null || echo 0)
	printf '%-14s %-6s %-8s %s\n' "$name" "$exitcode" "$lines" "$OUTDIR/$name.md"
done

echo
echo "council: git status after the run (should be clean if every agent behaved):"
git status --short || true

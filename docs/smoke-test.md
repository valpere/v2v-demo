# Manual smoke test

A scripted run through the demo. The point is to *hear* the voice and judge
the conversation — and, by covering a lot of ground, to make a gross error
unlikely to survive into a client demo.

This file is the **hub**: the shared Setup, the multi-topic picker checks
(§0), and the cross-topic robustness / clock / logging checks (§16–18). The
scenario sweep for each assistant lives in its own file under `docs/smoke/`:

| Topic | File | In the shipped manifest? |
| -- | -- | -- |
| Бюро перекладів (translation) | `docs/smoke/translation.md` | yes |
| Стоматологія «Перлина» (dental) | `docs/smoke/dental.md` | yes |
| Автосервіс «Гарант-Авто» (auto) | `docs/smoke/auto.md` | yes |
| Агенція нерухомості «Ключ» (realestate) | `docs/smoke/realestate.md` | yes |
| Клінінг «Свіжо» (cleaning) | `docs/smoke/cleaning.md` | yes |

**How to read a scenario.** Each step says what to **send** and what to
**expect** — compare against that. `[R]` = a regression case for a bug
already found and fixed; those must keep passing.

**Channel — text or voice?** Unless a step says otherwise, **send `code
font` as a typed / pasted text message, verbatim.** Text is the default: it's
faster and scriptable with `minions/tgdrive/`. The voice steps are called
out under their section headings.

## Setup

```bash
make run            # starts the bot (Ctrl-C to stop)
```

- There is **one chat**: you ↔ `@v2v_demo_bot`. Two separate reset actions —
  each step says which one it needs:
  - **Reset the session** — clears the session (topic, slots, history,
    language, escalated flag) **and re-arms the greeting / picker**: send
    **`/reset`**, then the *next* message gets the greeting (or the topic
    picker, if 2+ topics) again before its own reply. First-contact is
    derived from whether a session row exists — `/reset` deletes it, same as
    a chat the bot has never seen. `/start` re-sends the greeting/picker,
    clears nothing. Neither touches `data/`.
  - **Clear the logs** — with the bot stopped, `rm -f data/*.jsonl` (the bot
    recreates them). Do this when a step checks `data/leads.jsonl` for
    "one row" / "one new row".
  - **Full reset** = `/reset` **and** clear the logs.
  - **`SESSION_STORE`** (default `memory`) — a bot restart also clears the
    session under `memory`, same as `/reset`. Set `SESSION_STORE=sqlite`
    (`.env`) to have a restart resume mid-conversation instead — that
    changes the "restart" vs "`/reset`" distinction wherever a scenario
    relies on a restart.
  - **`TOPICS_PATH`** — the repo ships `topics/topics.json` with **two
    topics** (translation + dental), so `/start` **shows the picker by
    default**. Point `TOPICS_PATH` at a single-entry file to get the plain
    no-picker greeting; §0 below covers the picker checks and assumes 2+
    topics.
- The **first voice message ever** downloads the Whisper model (~1.5 GB) —
  that one turn takes minutes. After that ≈ 12 s STT + ~40 s LLM per voice
  turn on the dev backends (the client config is faster — see below).
- Keep a log tail open in another terminal:
  `tail -f data/turns.jsonl | jq .`   and   `cat data/leads.jsonl | jq .`
- **Watch the bot's console.** A reply that arrives as **text only** (no
  voice note) means TTS failed — `tts (chat …): …` on stderr says why.
  Same for `dialog: generator error …` / `dialog: no valid JSON response …`.
- Text turns can be scripted with `minions/tgdrive/` (drives an open,
  logged-in `web.telegram.org/a/` tab) — see `minions/TOOLS.md`. A single
  topic's flow can also be dry-run against the real pipeline with no
  Telegram: `go run ./minions/dialog-probe -topic <id> scenarios.txt`.
- **Client demo config** = `.env.client` flips: `STT_BACKEND=openai`
  (whisper-1), `DIALOG_BACKEND=openai` `gpt-4.1-mini` (D-20 — Ollama free
  tier is 13–86 s/turn; gpt-4.1-mini ~2–8 s), ElevenLabs **Starter** UA
  library voices. The translation sweep was verified on this stack
  2026-09-03 (see `docs/smoke/translation.md`).

---

## 0. Multi-topic mechanics

*Channel: **text**. The shipped `topics/topics.json` has 2+ topics, so the
picker is on by default. Point `TOPICS_PATH` at a single-entry file to
disable it — then `/start` sends the plain greeting and the bot is that one
topic from the first message, and this section is skipped.*

**0a — the picker on first contact.**

1. Full reset, then send `/start`.
   - **Expect:** a short prompt ("Оберіть тему розмови:") with **one inline
     button per topic**, in manifest order. **No** topic greeting yet, no
     `dialog.Handle` call — `data/turns.jsonl` gets no row.
2. Send `/start` again.
   - **Expect:** the picker again (idempotent).

**0b — picking a topic.**

1. Continuing from 0a, **tap** a topic button.
   - **Expect:** that topic's own greeting is sent; the tapped button stops
     spinning (it was acked). `data/turns.jsonl` still has no row (a pick is
     not a dialogue turn). In `data/` there's nothing to check, but the next
     message is answered by that topic's assistant.
2. Send a normal opening line for that topic.
   - **Expect:** a normal reply from that assistant, using **its** KB and
     persona (not another topic's).

**0c — a message before any topic is picked.**

1. Full reset, `/start` (picker shows), then send a text message **without
   tapping a button**, e.g. `Скільки це коштує?`.
   - **Expect:** the picker is **re-shown**; the message is **not** answered
     by any assistant; no turn record, no generator call.

**0d — switching topics mid-conversation.**

1. Pick topic A, fill a slot or two (send a couple of real answers), then
   send `/start` and **tap topic B**.
   - **Expect:** topic B's greeting. Then send a message: it's answered by
     **B**, and B starts from a **clean slate** — none of A's slots,
     history, or escalated flag carry over. (`/voice` preference **does**
     carry over — it's per-chat, not per-topic.)

**0e — a stale topic id (manifest changed under a live session).**

1. Pick a topic, then stop the bot, remove that topic's entry from
   `topics/topics.json`, restart, and send a message in the same chat.
   - **Expect:** the picker is shown (the persisted `Session.Topic` no
     longer resolves) — **not** a crash, **not** an answer from an empty
     KB / prompt.

**0f — `/voice` and `/reset` work before a topic is picked.**

1. Full reset, `/start` (picker shows), then send `/voice b`.
   - **Expect:** the "switched to the second voice" line — the command is
     **not** swallowed by the picker gate. Then `/voice a` → the "back to
     the first voice" line.
2. Send `/reset`.
   - **Expect:** "Сесію очищено." — then the next message re-shows the
     picker.

---

## 16. Robustness (upstream failures & odd inputs)

*Cross-topic — the transport / concurrency / degradation behaviour is the
same for every assistant. Examples below use the translation topic; nothing
here depends on which topic is active.*

*Channel: **text**, except where a step names an attachment type.*

Most of this section is **covered by Go tests** — deterministic, no second
Telegram account or killed backend needed. `make check` runs them; don't
re-do these by hand:

| # | Case | Covered by | Asserts |
| -- | -- | -- | -- |
| 16a | two messages back-to-back | `cmd/bot.TestPerChatFIFOOrdering` + verified live 2026-09-03 | processed in FIFO order; turn-1 slots survive into turn 2 |
| 16b | two chats at once | `cmd/bot.TestPerChatIsolation` | one chat escalates; the other keeps its slots, is not marked escalated, and never sees the handoff line |
| 16c | LLM backend down + recovery | `cmd/bot.TestGeneratorErrorNoCrashThenRecovers`, `dialog.TestHandleGeneratorError{,Ukrainian}` | degrade to the apology+handoff line, `TurnRecord` still written, no crash, next turn works once the backend is back; apology matches the turn language |
| 16d | bad TTS credential | `cmd/bot.TestTTSErrorFallsBackToText` | text reply still sent, `TurnRecord` still written, loop alive |
| 16f | model returns no valid JSON | `dialog.TestHandleNilTrailerEscalates` | fixed handoff line, `signal: escalate`, no crash |
| 16g | empty / whitespace input | `cmd/bot.TestWhitespaceInputNotADialogueTurn`, `telegram.TestToUpdate` | never reaches the generator; dropped at the transport boundary too |
| 16h | non-text attachments | `telegram.TestToUpdate` | document / sticker / video note / audio / captioned photo / edited message all yield "no update" — no hang, no panic, no empty voice note |
| 16i | degenerate short inputs (`5`, `👍`, `.`) | `cmd/bot.TestDegenerateShortInputsNoCrash` | no crash, a reply for each (whether it's *sensible* is an LLM check — worth a quick live glance before the demo) |

Still **live-only** — transport timing or LLM judgement, no deterministic test:

**16e — network drop.**

1. With the bot running, disconnect the network ~30 s, then reconnect.
   - **Expect:** long-polling reconnects on its own; **no** duplicate
     greeting, no repeated turn.
   - **Verified 2026-09-03** — `getUpdates` logged one
     `context deadline exceeded`, no panic/exit; a message sent ~2 min
     after reconnect was delivered and answered by the same process;
     greeting block appeared exactly once, one `TurnRecord`.

**16j — a huge rambling paragraph** (translation topic): see
`docs/smoke/translation.md` §16j — it's a slot-extraction stress case,
topic-specific.

---

## 17. Clock-dependent promises

*Cross-topic — `officeStatus` is the same for every assistant.*

The bot has no clock of its own — `cmd/bot` injects the current time (in
`BOT_TIMEZONE`, default `Europe/Kyiv`) into every prompt as a
`--- CURRENT TIME ---` block, and `dialog.officeStatus` (Go, not the model)
decides open/closed against Mon–Fri 09:00–18:00.

**Covered by Go tests:** `dialog.TestOfficeStatus` (weekday-midday open;
after 18:00 / before 09:00 / Sat / Sun closed),
`dialog.TestHandleInjectsCurrentTime` (the block reaches the prompt, open vs
closed wording), `cmd/bot.TestLoadConfigTimezoneOverride` and the "bad
timezone" validation case.

**Live check** (the LLM actually honouring the block):

1. During office hours, finish a full request on any topic, then ask
   `Коли зі мною зв'яжеться менеджер?`
   - **Expect:** "протягом ~15 хвилин".
2. After 18:00 EET or on a weekend, same thing (or set `BOT_TIMEZONE` to a
   zone where it's currently night, restart, run it any time).
   - **Expect:** "наступного робочого ранку" — **not** "15 хвилин".

---

## 18. Logging & state

*Cross-topic. Mostly **covered by Go tests** — the observable half you can
still eyeball in `data/*.jsonl` after any run.*

| Check | Covered by |
| -- | -- |
| normal dialogue turn → one `TurnRecord` (`time`, `chat_id`, `signal`, `latency_ms` populated) | `cmd/bot.TestTurnRecordOnlyForDialogueTurns`, `store.TestAppendTurn` |
| `lead_ready` turn → a `TurnRecord` **and** one `LeadRecord` (`{topic, fields}`) | `dialog.TestHandleLeadReady`, `dialog.TestHandleNoDuplicateLead`, `dialog.TestHandleCorrectionAfterLeadRecordsUpdatedLead`, `store.TestAppendLead` |
| `/voice …`, `/reset`, a topic pick, or an sttFail → **no** `TurnRecord` | `cmd/bot.TestTurnRecordOnlyForDialogueTurns` |
| pre-LLM escalate → `"matched": null` | `dialog.TestHandleHardEscalate`, `dialog.TestHandleNilTrailerEscalates` |
| normal grounded answer → `matched` lists the KB sections | `dialog.TestKBOverlap` + the per-topic live runs |
| a short reply after the bot asked a question → slot answer, **not** escalated despite zero KB overlap `[R]` | `dialog.TestIsSlotAnswer` |
| conversation past 20 turns → history trims, no error, the slot values survive (slot state is separate from history) | `dialog.TestHandleHistoryTrimKeepsSlots` |
| restart the bot mid-conversation → that chat's topic / slots / history are **gone** under `SESSION_STORE=memory`, **survive** under `SESSION_STORE=sqlite` | `internal/store.TestSQLiteSessionsRoundTrip` + concurrency/not-found tests. A quick live confirmation of either mode is fine. |

---

## What "pass" looks like

- **Voice** sounds natural on Ukrainian — Latin tokens, amounts, number
  ranges — and never reads markdown symbols aloud. This is the one thing the
  client judges (NFR-1).
- The picker appears once (with 2+ topics), each button loads the right
  assistant, and switching topics starts that assistant clean.
- Each assistant asks only for what it doesn't know, **never quotes a final
  total**, reads every slot back before `lead_ready`, and hands off cleanly
  on its out-of-scope and hard-escalation cases.
- **No fabrication** — not a value, a price, an address, or a policy the
  client or the KB didn't provide.
- **Language** never drifts to Russian; mid-conversation switches are
  followed; each reply is one language.
- An escalated chat is not bricked — the client can keep talking.
- Nothing crashes the loop — a failed STT / LLM / TTS call degrades to a
  text apology + handoff, and recovers on the next turn.
- Every `[R]` case still passes.

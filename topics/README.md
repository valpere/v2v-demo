# topics/ — multi-assistant manifest

`TOPICS_PATH` (default `topics/topics.json`) is a JSON array of topics.
**Each topic is a genuinely different assistant** — its own KB, system
prompt, greeting **and slot schema** (what it collects before it hands a
lead to a manager), not just a KB slice of the same persona.

This repo **ships `topics/topics.json`** with eight topics (translation,
dental, auto, realestate, cleaning, salon, barbershop, lyapko), so `/start` shows the inline-keyboard
picker. A manifest with a single entry turns the picker off (the bot then
behaves like a single-topic bot).

```json
[
  {
    "id": "translation",
    "title": "Бюро перекладів",
    "kb": "topics/translation/kb.md",
    "system_prompt": "topics/translation/system.md",
    "greeting": "topics/translation/greeting.md",
    "scope_uk": "Я допомагаю лише з перекладами документів.",
    "scope_en": "I only help with document translations.",
    "slots": [
      { "key": "language_pair", "ask_uk": "з якої мови на яку", "ask_en": "which languages", "rule": "e.g. uk->de" },
      { "key": "doc_type", "ask_uk": "який це документ", "ask_en": "what kind of document", "rule": "" }
      // …abbreviated — the shipped translation topic has 6 slots; see topics/topics.json
    ]
  }
]
```

> The `slots` array above is trimmed for readability. The real
> `topics/topics.json` translation entry declares all six
> (`language_pair`, `doc_type`, `volume`, `deadline`, `certification`,
> `delivery`) — don't copy this block over it.

- `id` — stable identifier, used internally (inline-button callback data,
  `Session.Topic`, the `topic` field of a lead record). Never shown to the
  user. A change to it orphans existing sessions on that topic (they
  re-show the picker).
- `title` — the inline-keyboard button label shown after `/start`
  (Ukrainian / primary).
- `title_en` — optional English half of the button label. When set (and
  different from `title`) the button reads `<title> · <title_en>` — the
  picker is the first interaction, before any language is known, so the
  labels and the prompt are bilingual. Blank → the button is `title` alone.
- `kb` / `system_prompt` / `greeting` — paths (relative to the bot's
  working directory) to that topic's own files, same formats as
  `KB_PATH` / `SYSTEM_PROMPT_PATH` / `GREETING_PATH`.
- `scope_uk` / `scope_en` — the one sentence the clarify line uses to say
  what this assistant is for ("Я допомагаю лише з …"). Required.
- `office` — this assistant's business hours, injected into the
  `--- CURRENT TIME ---` prompt block so the model promises "within about
  15 minutes" while open and "the next business morning" while closed.
  `label` is the English string shown in that block (e.g.
  `"Mon–Fri 09:00–18:00 (EET)"`); `weekday` / `sat` / `sun` are
  `[open, close]` hours (24h) for those days, `[0, 0]` or omitted meaning
  closed that day. **Omitting `office` entirely** means Mon–Fri
  09:00–18:00 (the translation bureau's hours). The open/closed decision is
  made in Go against `BOT_TIMEZONE`, not by the model.
  `reply_within` (optional, English, e.g. `"about 15 minutes"`) is the
  response time the assistant may promise **while open**; empty = it
  promises no specific time (only translation and cleaning state one in their
  KB). While closed it always says "the next business morning".
  `closed_note_uk` / `closed_note_en` (optional) — a sentence appended to
  the fixed handoff line **on an `escalate` while the office is closed**,
  for the one thing a client who needs help now must be told when no human
  is available (e.g. a clinic pointing at emergency services). Picked by the
  conversation language; the model's own reply is never spoken on escalate,
  so this is the only way to add such a line.
- `emergency` (optional) — deterministic emergency handling: `patterns`
  (case-insensitive regexps matched against the user's text) and
  `reply_uk` / `reply_en` (both required when patterns are set). A match gets
  that fixed text followed by the normal handoff line — **always, open or
  closed, before and instead of the model**. Invalid regexps fail at load.
  Used by dental (bleeding, breathing/swallowing, spreading swelling, facial
  trauma → 103 / 112); the wording is medical, so change it deliberately.
- `escalate_rules` (optional) — deterministic handoffs for rules a prompt
  cannot be trusted with: `{if, and, unless, slot}` regexps. Fires (plain
  handoff, before the model) when `if` matches the message plus the named
  slot's value (so an EV named a turn earlier still counts), `and` matches the
  message, and `unless` does not. Auto uses it: an electric vehicle is served
  for tyres and alignment only.
- `slots` — an **ordered** list of what the assistant collects. `key` is
  the JSON key the model returns it under (and the key in the lead record);
  `ask_uk` / `ask_en` are the plain-words phrasings the clarify line uses
  when that slot is still missing; `rule` is an optional one-line
  constraint hint injected into the response-format block. A `lead_ready`
  signal is only honoured once **every** slot has a value. At least one
  slot; keys must be unique within a topic.

**Fallback (used only when the manifest is missing or empty).** The repo
ships a `topics/topics.json` with several topics, so the picker is on by
default. If `TOPICS_PATH` points at nothing, or the file parses to an empty
array, the bot falls back to a single synthetic topic built from `KB_PATH` /
`SYSTEM_PROMPT_PATH` / `GREETING_PATH` and the translation slot schema — no
picker. A one-entry `topics.json` is treated the same way (no picker), it
just loads from that entry's own paths.

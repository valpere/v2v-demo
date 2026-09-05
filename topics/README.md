# topics/ — multi-assistant manifest (optional)

`TOPICS_PATH` (default `topics/topics.json`) points at a JSON array of
topics. **Each topic is a genuinely different assistant** — its own KB,
system prompt and greeting, not just a different KB slice of the same
persona.

```json
[
  {
    "id": "translations",
    "title": "Переклад документів",
    "kb": "kb/translation-bureau.md",
    "system_prompt": "prompt/system.md",
    "greeting": "prompt/greeting.md"
  }
]
```

- `id` — stable identifier, used internally (inline-button callback data,
  `Session.Topic`). Never shown to the user.
- `title` — the inline-keyboard button label shown after `/start`.
- `kb` / `system_prompt` / `greeting` — paths to that topic's own files,
  same formats as `KB_PATH` / `SYSTEM_PROMPT_PATH` / `GREETING_PATH`.

**This feature is opt-in.** If `topics.json` is missing (the default —
this repo ships only `topics.json.example`), or has exactly one entry, the
bot auto-selects that single topic and never shows a picker: behavior is
byte-for-byte what a bot with no manifest at all does, built straight from
`KB_PATH` / `SYSTEM_PROMPT_PATH` / `GREETING_PATH`. The picker only
appears once `topics.json` lists two or more entries — copy
`topics.json.example` to `topics.json` and add topics to turn it on.

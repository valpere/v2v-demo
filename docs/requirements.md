# Requirements — v2v-demo

Traced requirements for this throwaway demo. Written in **tumanomir's
traceable markup** (`[REQ-*] -> [FUN-*] / [LOG-*] / [PHY-*]`, `@schema`,
`@constraint`) so `tumanomir check` can measure trace coverage (`K_drift`)
and constraint density (`D_const`) over this file.

A bracketed `[REQ-*]` at the start of a numbered item **defines** a
requirement; a `-> [FUN-*]` / `-> [LOG-*]` / `-> [PHY-*]` line before the next
`[REQ-*]` is its trace edge. Bare mentions in prose (e.g. "see REQ-DLG-01")
are cross-references, deliberately unbracketed.

`docs/architecture.md` is the *shape*; `.agents/plan.md` §"Type surface" is the
authoritative Go signature list and the traces below point into it. Full
engagement references for the S1–S5 keys are in `.engage/` (local only — this
repo is public).

---

## 1. Data model

@schema Config {
  telegram_token:   String @constraint(rule: "required"),
  tts_backend:      Enum["elevenlabs","azure"] @constraint(default: "elevenlabs"),
  eleven_key:       String,
  eleven_voice_a:   String,
  eleven_voice_b:   String,
  azure_key:        String,
  azure_region:     String,
  azure_voice_a:    String @constraint(default: "uk-UA-PolinaNeural"),
  azure_voice_b:    String @constraint(default: "uk-UA-OstapNeural"),
  stt_backend:      Enum["local","openai"] @constraint(default: "local", rule: "dev default: local (openai-whisper CLI) is free + needs no key. The client-facing recording (I-10) MUST flip to openai — local Whisper on a CPU box is tens of seconds to minutes and fails REQ-NFR-02 live (B2 substance kept, its default-flip reverted)"),
  whisper_bin:      String @constraint(default: "whisper", rule: "the openai-whisper CLI (pipx-installed); used only when stt_backend=local"),
  whisper_model:    String @constraint(default: "medium", rule: "openai-whisper model NAME (tiny|base|small|medium|large-v3|turbo), auto-downloaded to ~/.cache/whisper on first use; used only when stt_backend=local"),
  whisper_lang:     Enum["auto","uk","en"] @constraint(default: "auto"),
  dialog_backend:   Enum["ollama","openai","gemini"] @constraint(default: "ollama", rule: "gemini was the intended default but needs a $25 prepay on the AI Studio project (429 'prepayment credits depleted', 2026-08-29) — demoted to last resort; ollama gemma4:cloud is the verified default"),
  dialog_model:     String @constraint(default: "gemma4:cloud"),
  gemini_key:       String @constraint(rule: "required when dialog_backend=gemini"),
  ollama_base_url:  String @constraint(default: "http://localhost:11434"),
  openai_key:       String @constraint(rule: "required when stt_backend=openai (the I-10 client recording) or dialog_backend=openai; not needed for the dev default"),
  kb_path:          String @constraint(default: "kb/translation-bureau.md"),
  system_prompt_path: String @constraint(default: "prompt/system.md"),
  greeting_path:    String @constraint(default: "prompt/greeting.md"),
  data_dir:         String @constraint(default: "./data")
}

@schema GateParams {
  gate_floor:          Float @constraint(default: 0.25, rule: "kbOverlap (fraction of meaningful query terms found in the WHOLE KB) below this on a content question -> escalate before the LLM"),
  history_limit:       Int   @constraint(default: 20, rule: "Session.History cap: 20 Msg entries (about 10 turns), NOT 20 turns"),
  slot_answer_max_tok: Int   @constraint(default: 6, rule: "isSlotAnswer: an answer to a slot question is at most this many tokens"),
  recording_tick:      Duration @constraint(default: "4s", rule: "cmd/bot re-sends the Telegram recording action at this interval")
  @constraint(rule: "package-level consts in internal/dialog and cmd/bot; not runtime config. B1: there is NO retrieval config — the whole KB goes in the prompt")
}

@schema QuoteSlots {
  language_pair:  String? @constraint(rule: "e.g. uk->de"),
  doc_type:       String?,
  volume:         String?,
  deadline:       String?,
  certification:  String? @constraint(rule: "none | certified | notarized | sworn"),
  delivery:       String?
  @constraint(rule: "complete = all six non-null; drives the lead_ready signal")
}

@schema Trailer {
  slots:  QuoteSlots @constraint(rule: "all six keys present; null when unknown; merge only non-null keys learned this turn"),
  signal: Enum["continue","lead_ready","escalate"] @constraint(default: "continue", rule: "missing or unparseable trailer -> escalate")
}

@schema Session {
  slots:     QuoteSlots,
  history:   List<Msg> @constraint(rule: "trimmed to the last 20 Msg entries (about 10 turns); lost on restart"),
  voice:     Enum["a","b"] @constraint(default: "a"),
  lang:      Enum["uk","en"] @constraint(rule: "locked from the first non-empty user turn; the fallback for handoff/apology lines when the current turn's language is undetermined"),
  escalated: Bool @constraint(default: false)
}

@schema Msg {
  role: Enum["user","assistant"],
  text: String
}

@schema TurnRecord {
  time:       Timestamp,
  chat_id:    Int,
  user_text:  String,
  reply_text: String,
  signal:     String,
  matched:    List<String> @constraint(rule: "titles of the KB sections used this turn; empty on a pre-LLM escalate"),
  slots:      QuoteSlots @constraint(rule: "snapshot of Session.Slots after this turn's merge — the slot-change trail"),
  latency_ms: Int
}

@schema LeadRecord {
  time:          Timestamp,
  chat_id:       Int,
  language_pair: String,
  doc_type:      String,
  volume:        String,
  deadline:      String,
  certification: String,
  delivery:      String
  @constraint(rule: "the shape a Zoho lead would take; written to data_dir/leads.jsonl, sent nowhere")
}

---

## 2. Sources (traceability keys)

Kept generic here; full references in `.engage/sources.md`.

- **S1** — the client's written brief (system description, application
  questions, prototype checklist, GDPR section).
- **S2** — the pre-engagement chat: the client wants to *hear* the assistant,
  any topic is fine, agreed format is a Telegram bot with voice messages + a
  sample recording.
- **S3** — the agreed demo scope: neutral scenario, two voice options, a
  structured lead in Zoho-field shape (no real connection), a short fixed
  timeline, credited toward a later build; plus the explicit "not in the demo"
  list.
- **S4** — the chosen stack: Go, Telegram long-polling, local Whisper STT
  (dev) / `whisper-1` (client recording) + Ollama `gemma4:cloud` + ElevenLabs
  multilingual v2, with config-flag alternates (`gpt-4o-mini` / Gemini Flash,
  Azure Neural). See `docs/architecture.md`.
- **S5** — the project's coding conventions: Go for user-facing tools, GDPR
  awareness, never commit secrets, Ukrainian for user-facing copy.

---

## 3. Scope

In: a Telegram bot that holds a spoken quote-gathering conversation for a
fictional bureau, in Ukrainian and English (no RU — REQ-DLG-05), on fabricated
data, hosted so the client can poke at it, plus a short recorded sample
dialogue. Out: everything in §6.

---

## 4. Functional requirements

### 4.1 Channel — Telegram

1. [REQ-TG-01] The bot must accept an incoming Telegram **voice message** and
   obtain the audio as a local OGG file.
   -> [FUN-TG-01] telegram.Client.Updates yields Update{VoiceFileID}; telegram.Client.DownloadVoice(ctx, fileID) (oggPath, error)

2. [REQ-TG-02] The bot must accept a plain **text** message and route it into
   the same dialogue path as a transcript, so the demo is testable without
   recording audio.
   -> [FUN-TG-02] telegram.Client.Updates yields Update{Text}; cmd/bot update loop calls dialog.Handle with Update.Text unchanged

3. [REQ-TG-03] The bot must reply with a Telegram **voice message** (the
   synthesized answer) and, **once**, the same text as a normal message so it
   is readable. The text must not also be attached as a voice caption.
   -> [FUN-TG-03] telegram.Client.SendVoice(ctx, chatID, ogg) — no caption — then one SendText(ctx, chatID, reply.Text); ogg from tts.Synthesizer.Speak

4. [REQ-TG-04] The Telegram "recording voice" chat action must stay visible
   for the whole STT -> LLM -> TTS chain (server-side it lasts ~5 s, so it
   must be re-sent).
   -> [FUN-TG-04] cmd/bot update loop re-calls telegram.Client.SendRecordingAction on a ~4 s ticker until the turn completes or ctx is cancelled

### 4.2 Speech-to-text

5. [REQ-STT-01] The bot must transcribe a voice message to text. Dual-mode:
   the dev / code default is `stt_backend=local` — the `openai-whisper` CLI
   (`whisper`), which ingests the OGG directly via its own ffmpeg call (no
   manual conversion), model name from `whisper_model` (default `medium`).
   Free, no key. The **client-facing recording (I-10) MUST use
   `stt_backend=openai`** (`whisper-1` API, ~2–4 s): local Whisper on a CPU
   box (30 s–minutes at `medium`) alone blows REQ-NFR-02 in a live setting
   (B2). A failure never auto-switches backends — only the config flag does.
   -> [FUN-STT-01] stt.Transcriber.Transcribe(ctx, oggPath, langHint); impls stt.NewLocal (default), stt.NewOpenAI, selected by Config.stt_backend

### 4.3 Dialogue core

6. [REQ-DLG-01] Each user turn must produce one reply generated by an LLM
   given the whole KB and the rolling conversation history, not a scripted
   response.
   -> [FUN-DLG-01] dialog.Handle(ctx, sess, kb, gen, systemPrompt, userText) (Reply, error)

7. [REQ-DLG-02] The assistant must ask relevant clarifying questions that move
   the conversation toward a quote, asking only about the quote parameters not
   yet known.
   -> [LOG-DLG-02] prompt/system.md "conversation playbook" section defines the intake order; dialog.Handle passes the current QuoteSlots as "COLLECTED SO FAR" so the model asks only about nil fields

8. [REQ-DLG-03] The assistant must collect six quote parameters:
   `language_pair`, `doc_type`, `volume`, `deadline`, `certification`,
   `delivery` (see @schema QuoteSlots).
   -> [FUN-DLG-03] dialog.QuoteSlots struct; the model returns them in the fenced Trailer, dialog.Handle merges non-null keys into sess.Slots

9. [REQ-DLG-04] When all six parameters are known, the assistant must
   summarize them back, tell the client a manager will send the quote, emit
   the `lead_ready` signal, and the bot must append one LeadRecord.
   -> [FUN-DLG-04] dialog.QuoteSlots.Complete() gates SignalLeadReady; store.AppendLead(dataDir, LeadRecord) on that signal

10. [REQ-DLG-05] The assistant must detect the user's language (Ukrainian or
    English) from the first turn, converse in it, and follow a mid-dialogue
    switch. Russian is out of demo scope: not tested and not prompted for.
    -> [LOG-DLG-05] prompt/system.md "Language" section: detect uk/en, mirror the user, one language per message; no RU test case in examples/dialogues.md

11. [REQ-DLG-06] The assistant must hand off to a human — reply with a fixed
    handoff line and set `sess.Escalated` — on any of: the user asks for a
    person or is unhappy; sworn translation for a language the KB does not
    list; a legal / liability / admissibility question; a complaint about
    delivered work; a payment or refund dispute; an interpreting booking; or
    a content question the KB does not cover.
    -> [FUN-DLG-06] dialog.Signal == SignalEscalate path in dialog.Handle sets sess.Escalated, substitutes the handoff line; escalate list enumerated in prompt/system.md "Hard rules"

12. [REQ-DLG-07] The LLM call must go through a `Generator` interface with
    three interchangeable implementations — `ollama` (default), `openai`,
    `gemini` — selected by `dialog_backend`; switching must need no code
    change.
    -> [FUN-DLG-07] dialog.Generator.Generate(ctx, systemPrompt, history); impls dialog.NewOllama, dialog.NewOpenAI, dialog.NewGemini chosen by Config.dialog_backend

13. [REQ-DLG-08] The model response must be parsed as spoken reply text
    followed by one fenced ```json Trailer; the trailer must be stripped
    before the text is spoken; a missing or unparseable trailer must be
    treated as `signal: escalate` and logged.
    -> [FUN-DLG-08] dialog.Handle splits on the last ```json fence; json.Unmarshal into dialog.trailer; on error -> Reply{Signal: SignalEscalate}

14. [REQ-DLG-09] The slot merge must never clear an already-filled slot unless
    the user explicitly corrected it, and must record every slot change in the
    turn record.
    -> [LOG-DLG-09] dialog.Handle merge step: for each Trailer.Slots key, assign into sess.Slots only when the incoming value is non-nil; TurnRecord carries the resulting signal and matched titles

### 4.4 Grounding (the coherence guarantee)

The algorithms are given step-for-step in `.agents/plan.md`
§"Behavioural spec (pseudocode)"; the trace edges below point into it and the
named constants are `@schema GateParams` in §1.

15. [REQ-DLG-10] The **whole KB** goes into every system prompt — each section
    as `## Title` then body — with no retrieval or truncation (B1: the KB is
    ~6 KB, well inside the model's context). The only score computed is
    `kbOverlap` = the fraction of the user message's meaningful (stopword-
    filtered) terms that appear anywhere in the concatenated KB; it feeds the
    grounding gate and nothing else.
    -> [FUN-DLG-10] dialog.kbOverlap(query string, kb []Section) float64 — deterministic; the KB is passed to the Generator in full by dialog.Handle step 5

16. [REQ-DLG-11] `kbOverlap` tokenisation lowercases, strips punctuation,
    whitespace-splits, and drops a fixed package-level stopword set (~40
    Ukrainian + English function words). An empty query after filtering yields
    `kbOverlap = 0`. Exact match, no stemming — if inflected Ukrainian forms
    are missed in testing, add a stemmer (B1 fallback), do not reintroduce
    per-section retrieval.
    -> [LOG-DLG-11] dialog stopwords var (map[string]bool); rationale: function words inflate the overlap so the gate never fires (ragline's tsquery lesson)

17. [REQ-DLG-12] A turn is a **slot answer** when the user message is at most
    `GateParams.slot_answer_max_tok` tokens, **the most recent assistant
    message ends with "?"**, and a quote slot is still nil (B3: the
    "bot just asked" clause stops a bare "apostille?" from bypassing the gate).
    -> [FUN-DLG-12] dialog.isSlotAnswer(sess *Session, userText string) bool

18. [REQ-DLG-13] The grounding gate decides, from `(kbOverlap, slotAnswer)`
    only: a slot answer never escalates; otherwise `kbOverlap` below
    `GateParams.gate_floor` forces escalation **before** any Generator call;
    otherwise the turn proceeds. With `hardEscalate` (REQ-DLG-20) it is the
    anti-waffle layer in front of the LLM.
    -> [FUN-DLG-13] dialog.groundingGate(overlap float64, slotAnswer bool) (forceEscalate bool) @constraint(rule: "pure function of its two arguments; no I/O, no session read")

19. [REQ-DLG-14] `dialog.Handle` must execute the exact 14-step sequence in
    plan.md §"Behavioural spec": lock `Session.Lang` -> `hardEscalate` check
    (early handoff) -> classify slot answer -> `kbOverlap` -> grounding gate
    (early handoff) -> build the system prompt (`system` file + `--- KNOWLEDGE
    BASE ---` + **the whole KB**, each section as `## Title` then body +
    `--- COLLECTED SO FAR ---` + compact slot JSON) -> append user msg, trim
    to `history_limit` -> Generate -> parse trailer -> merge slots (6 explicit
    field assignments) -> resolve signal (incl. the B4 guard) -> append
    assistant msg -> return Reply.
    -> [FUN-DLG-14] dialog.Handle(ctx, sess *Session, kb []Section, gen Generator, systemPrompt, userText string) (Reply, error)

20. [REQ-DLG-15] Trailer parsing must take the **last** fenced block whose
    opening fence is ```json / ```JSON / a bare ``` immediately followed by a
    line starting with `{`, JSON-parse it into a Trailer, and validate
    `signal ∈ {continue, lead_ready, escalate}`. A missing block, a JSON
    error, or an unknown signal all yield `tr = nil`. **The raw model output
    is never spoken** — a `nil` trailer makes `dialog.Handle` reply with the
    fixed handoff line and escalate (see REQ-DLG-16).
    -> [FUN-DLG-15] dialog.parseTrailer(raw string) (spoken string, tr *trailer)

21. [REQ-DLG-16] Signal resolution: `tr == nil` -> escalate, reply text is the
    fixed handoff line, logged. Otherwise the signal is `tr.Signal` verbatim,
    with one guard (B4): a `lead_ready` while `QuoteSlots.Complete()` is false
    is downgraded to `continue` and a warning logged. There is **no
    `continue`->`lead_ready` upgrade** — the model owns the positive case and
    the system prompt ties it to the read-back summary (REQ-DLG-04). Any
    `escalate` (trailer, `hardEscalate`, gate, or parse failure) sets
    `Session.Escalated` and replaces the spoken text with the handoff line.
    -> [LOG-DLG-16] dialog.Handle steps 1, 4, 9, 11 — every escalate path substitutes handoffLine(sessLang(sess)); step 11 has the lead_ready-vs-Complete guard

22. [REQ-DLG-17] Any `Generator.Generate` error must be caught inside
    `dialog.Handle` and turned into a `Reply{Signal: escalate}` with a fixed
    apology line in the session language — the error is never returned to the
    caller and never crashes the update loop (see REQ-NFR-03).
    -> [LOG-DLG-17] dialog.Handle step 6: err path sets Session.Escalated, returns Reply{Text: apologyLine(sessLang(sess))}, nil error

23. [REQ-DLG-18] The assistant must never state a final total price; it
    collects parameters and defers the quote to a manager. Ranges quoted from
    the KB are allowed; a computed or committed total is not.
    -> [LOG-DLG-18] prompt/system.md "Hard rules"; kb/translation-bureau.md "How a price is formed" repeats it in-domain

24. [REQ-DLG-19] `langOf(text)` returns `"uk"` for any Cyrillic rune, `"en"`
    for a Latin letter with no Cyrillic, `""` when neither (digits/punctuation
    only). `dialog.Handle` locks `Session.Lang` from the first turn where
    `langOf` is non-empty; the fixed handoff / apology lines use
    `sessLang(sess)` = `Session.Lang` or `"en"`. So a mid-conversation STT
    failure still yields a line in the conversation's language. The
    *conversation* language stays the model's responsibility (REQ-DLG-05).
    -> [FUN-DLG-19] dialog.langOf(text string) string; dialog.sessLang(sess *Session) string

25. [REQ-DLG-20] `hardEscalate(query)` returns true when the lowercased message
    contains any entry of a short fixed keyword list (uk+en) for unambiguous
    handoff topics — refund / повернення коштів, complaint / скарга, court /
    суд / позов, "talk to a person" / дайте людину / справжня людина, a direct
    request for a manager. A true result forces a handoff **before** the slot-
    answer bypass and the gate (B3). Two-term cases (sworn + non-listed
    language; interpreting + booking) stay with the model's own `escalate`.
    -> [FUN-DLG-20] dialog.hardEscalate(query string) bool; dialog escalateKeywords []string

### 4.5 Knowledge base

26. [REQ-KB-01] The KB file must be loaded and split into titled sections on
    its `## ` headings; the assistant answers service questions only from
    these sections.
    -> [FUN-KB-01] kb.Load(path) ([]Section, error); Section{Title, Body}

### 4.6 Text-to-speech

27. [REQ-TTS-01] The bot must synthesize the reply text to OGG/Opus mono
    through a `Synthesizer` interface with two implementations —
    `elevenlabs` (`eleven_multilingual_v2`, default) and `azure`
    (`uk-UA-*Neural`) — selected by `tts_backend`.
    -> [FUN-TTS-01] tts.Synthesizer.Speak(ctx, text, voiceID, lang) ([]byte, error); impls tts.NewElevenLabs, tts.NewAzure chosen by Config.tts_backend

### 4.7 Session controls & first contact

28. [REQ-UX-01] `/voice a` and `/voice b` must switch, for that chat only,
    between the two configured voices; the choice persists on the session.
    -> [FUN-UX-01] cmd/bot update loop handles the /voice command by setting Session.voice; tts voiceID resolved from Config.{eleven,azure}_voice_{a,b}

29. [REQ-UX-02] The bot's first message in a chat (on `/start` or the first
    inbound message) must state that this is a demo and that the conversation
    is logged. Fixed bilingual text, not LLM-generated.
    -> [FUN-UX-02] cmd/bot sends the body of prompt/greeting.md once per chat, gated on Update.IsStart or an unseen chat id @constraint(rule: "the sent text is the file content after the first line that is exactly '---', with further '---' lines dropped and the result trimmed — the header is never sent")

### 4.8 Logging

30. [REQ-LOG-01] Every turn must be appended to a JSONL log as a TurnRecord
    (user text, reply text, signal, matched section titles, slot snapshot,
    latency). A lead record is appended to a second JSONL file iff the turn's
    signal is `lead_ready`.
    -> [FUN-LOG-01] store.AppendTurn(dataDir, TurnRecord) after each dialog.Handle; store.AppendLead(dataDir, LeadRecord) on lead_ready

### 4.9 Tests

31. [REQ-TST-01] The pure functions must have table-driven unit tests that run
    with no network and no API keys: `kbOverlap` (fraction, empty query,
    stopword filtering, no stemming), `hardEscalate` (each keyword, negative
    case), `groundingGate` (all four input combinations), `isSlotAnswer`
    (short+question+nil, short-no-question, long), `parseTrailer` (each fence
    spelling, JSON error, unknown signal, no trailer), the 6-way slot merge
    (non-nil overwrite, nil never clears), the B4 guard, `QuoteSlots.Complete`,
    `langOf`, `sessLang`, `greetingBody`.
    -> [LOG-TST-01] internal/dialog/*_test.go, internal/kb/*_test.go; `go test ./... -race` in the build order after each step (AGENTS.md)

### 4.10 The cmd/bot update loop

32. [REQ-BOT-01] The update loop must, per inbound update: send the greeting
    body once per chat (on `/start` or an unseen chat id); obtain the text
    (STT for voice, `Update.Text` for text); handle `/voice a|b` locally;
    otherwise call `dialog.Handle`; `tts.Speak` the reply; `SendVoice` (no
    caption) then one `SendText(reply.Text)`; `store.AppendTurn` **always**;
    `store.AppendLead` iff `reply.Signal == lead_ready`.
    -> [FUN-BOT-01] cmd/bot handleUpdate — see plan.md §"cmd/bot update loop"

33. [REQ-BOT-02] Concurrency: one goroutine per update; a `sync.Mutex` guards
    the `map[chatID]*Session` and the per-chat lock map; each chat's turns run
    strictly serially (per-chat lock) while different chats run concurrently.
    A panic in one turn's goroutine is recovered and logged — it never stops
    the loop.
    -> [LOG-BOT-02] cmd/bot: sessMu sync.Mutex; chatLocks map[int64]*sync.Mutex; defer recover in handleUpdate

34. [REQ-BOT-03] On an STT error or an empty/whitespace transcript, the loop
    must reply with the fixed `sttFailLine` in the session language and
    **not** call `dialog.Handle`, append a turn record, or advance any slot.
    -> [FUN-BOT-03] cmd/bot handleUpdate STT branch

35. [REQ-BOT-04] `telegram.Client.Updates` must advance the `getUpdates`
    offset only after an update is delivered on its channel. A crash before
    that re-delivers the update (a repeated greeting or turn, at worst a
    duplicate lead in the log) — acceptable for a demo; no persistence is
    added (deferred, §6).
    -> [PHY-BOT-04] internal/telegram long-poll: offset = last delivered update_id + 1

---

## 5. Non-functional requirements

36. [REQ-NFR-01] Voice output must sound natural, not robotic — this is the
    single evaluation criterion. The two configured voices must read Ukrainian
    cleanly, including Latin surnames and EUR amounts. The Ukrainian library
    voices are used for the client-facing recording (needs ElevenLabs Starter —
    Free rejects library voices via API); dev runs on premade Sarah/George.
    Azure Neural is the free fallback, a notch below ElevenLabs.
    -> [LOG-NFR-01] tts_backend default "elevenlabs" with model eleven_multilingual_v2; dev voice IDs premade (Sarah/George), UA library IDs per .engage/inventory.md I-4 swapped in on Starter

37. [REQ-NFR-02] Voice-in to voice-out latency should target under ~10 s; the
    recording chat action (REQ-TG-04) must be shown while the chain runs.
    -> [LOG-NFR-02] no streaming; the chain is STT then one Generator call then one Speak call; SendRecordingAction precedes it

38. [REQ-NFR-03] Any single upstream failure (STT, Generator, Synthesizer,
    Telegram) must degrade to a text apology and the loop must keep serving
    other chats — never crash.
    -> [LOG-NFR-03] cmd/bot recovers a panic per handler goroutine; dialog/tts errors return a Reply/handoff text, not a process exit; see docs/architecture.md §8

39. [REQ-NFR-04] No real personal data anywhere: the KB is fictional; the only
    personal data in the logs is the client's own test messages.
    -> [LOG-NFR-04] kb/translation-bureau.md is invented ("FromToBridge"); no ingestion of external data; TurnRecord stores only what the tester typed

40. [REQ-NFR-05] Secrets must come only from the environment (or a local
    `.env`); never in code, logs, or git.
    -> [LOG-NFR-05] Config populated by cmd/bot.LoadConfig from env; .env is gitignored; log statements never print a full key

41. [REQ-NFR-06] All bot-authored copy must be in the user's language
    (Ukrainian by default); code, identifiers and comments in English.
    -> [LOG-NFR-06] prompt/system.md and prompt/greeting.md are UA/EN; Go source is English per S5

42. [REQ-NFR-07] The bot must be reachable by the client while they evaluate
    it. It runs locally for now; an always-on host is a step before an
    unattended client link.
    -> [PHY-NFR-07] cmd/bot is a long-poll process with no inbound port; deployment target deferred (see .engage/inventory.md I-9)

---

## 6. Deferred — explicitly NOT in the demo

Real Zoho CRM connection; telephony (inbound calls, recording, transcription,
warm transfer, a phone number); real-time full-duplex voice; any database
(Postgres, SQLite, pgvector, FTS); other channels (web widget, WhatsApp);
self-service KB editing; persistence across restart; auth; multi-tenant; real
analytics dashboards; Google Cloud TTS (the `Synthesizer` interface should
accept a third impl, but do not build it); the full GDPR data architecture
(the demo shows only the consent line REQ-UX-02 and uses no real data). The
MVP substrate is in `docs/architecture.md` §7.

---

## 7. Inputs & materials

Accounts, keys and authored assets (I-1 … I-15) with status are in
`.engage/inventory.md` (local only). Authored and done:
`kb/translation-bureau.md`, `prompt/system.md`, `prompt/greeting.md`,
`examples/dialogues.md`.

---

## 8. Open questions

None blocking implementation. Verification still pending: none — the Gemini
test that was pending is closed as *blocked* (the AI Studio project returns
429 "prepayment credits are depleted" on every model, 2026-08-29). Gemini
stays a last-resort config-flag alternate (needs a $25 prepay on the AI Studio
project); revisit as the default only if that is paid and a Ukrainian dialogue
test then passes.

**Resolved:** dialogue default Ollama `gemma4:cloud` (verified on a Ukrainian
dialogue test), with `gpt-4o-mini` then Gemini Flash as alternates; STT
dual-mode — local Whisper for dev, `whisper-1` a mandatory flip for the I-10
client recording; ElevenLabs Free
during build (premade Sarah/George — library/UA voices need Starter) then
Starter + UA library voices before the client link, Azure the free fallback;
fictional bureau "FromToBridge"; Ukrainian + English, no Russian; host local
for now.

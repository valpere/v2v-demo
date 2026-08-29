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
  stt_backend:      Enum["local","openai"] @constraint(default: "local"),
  whisper_bin:      String @constraint(default: "whisper-cli"),
  whisper_model:    String @constraint(rule: "ggml model path; required when stt_backend=local"),
  whisper_lang:     Enum["auto","uk","en"] @constraint(default: "auto"),
  dialog_backend:   Enum["gemini","ollama","openai"] @constraint(default: "gemini"),
  dialog_model:     String @constraint(default: "gemini-flash-latest"),
  gemini_key:       String @constraint(rule: "required when dialog_backend=gemini"),
  ollama_base_url:  String @constraint(default: "http://localhost:11434"),
  openai_key:       String @constraint(rule: "required only on the openai rollback path"),
  kb_path:          String @constraint(default: "kb/translation-bureau.md"),
  system_prompt_path: String @constraint(default: "prompt/system.md"),
  greeting_path:    String @constraint(default: "prompt/greeting.md"),
  data_dir:         String @constraint(default: "./data")
}

@schema RetrieveParams {
  score_floor:         Float @constraint(default: 0.15, rule: "min BM25-lite score to count as relevant; also the grounding-gate escalation threshold"),
  top_k:               Int   @constraint(default: 3, rule: "max sections passed to the LLM"),
  title_weight:        Float @constraint(default: 3.0, rule: "a query term in a section title counts this many body occurrences"),
  history_limit:       Int   @constraint(default: 20, rule: "Session.History cap in Msg entries, not turns"),
  slot_answer_max_tok: Int   @constraint(default: 6, rule: "isSlotAnswer: an answer to a slot question is at most this many tokens")
  @constraint(rule: "package-level consts in internal/dialog; not runtime config")
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
  history:   List<Msg> @constraint(rule: "trimmed to the last 20 turns; lost on restart"),
  voice:     Enum["a","b"] @constraint(default: "a"),
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
  matched:    List<String> @constraint(rule: "titles of the KB sections used this turn"),
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
- **S4** — the chosen stack: Go, Telegram long-polling, local Whisper +
  Gemini Flash / Ollama `gemma4:cloud` + ElevenLabs multilingual v2, with
  config-flag alternates. See `docs/architecture.md`.
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

3. [REQ-TG-03] The bot must reply with a Telegram **voice message** carrying
   the synthesized answer, and also send the same text as a normal message so
   it is readable.
   -> [FUN-TG-03] telegram.Client.SendVoice(ctx, chatID, ogg, text) then SendText; ogg from tts.Synthesizer.Speak

4. [REQ-TG-04] While the STT -> LLM -> TTS chain runs, the bot must show the
   Telegram "recording voice" chat action.
   -> [FUN-TG-04] telegram.Client.SendRecordingAction(ctx, chatID) called before the chain, once per turn

### 4.2 Speech-to-text

5. [REQ-STT-01] The bot must transcribe a voice message to text. The default
   path is **local** (`ffmpeg` OGG->WAV, then `whisper-cli` with a ggml model);
   `stt_backend=openai` selects the `whisper-1` API. A local failure must not
   auto-fall back to the API — only the config flag switches backends.
   -> [FUN-STT-01] stt.Transcriber.Transcribe(ctx, oggPath, langHint); impls stt.NewLocal, stt.NewOpenAI selected by Config.stt_backend

### 4.3 Dialogue core

6. [REQ-DLG-01] Each user turn must produce one reply generated by an LLM
   given the retrieved KB sections and the rolling conversation history, not a
   scripted response.
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
    three interchangeable implementations — `gemini` (default), `ollama`,
    `openai` — selected by `dialog_backend`; switching must need no code
    change.
    -> [FUN-DLG-07] dialog.Generator.Generate(ctx, systemPrompt, history); impls dialog.NewGemini, dialog.NewOllama, dialog.NewOpenAI chosen by Config.dialog_backend

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
named constants are `@schema RetrieveParams` in §1.

15. [REQ-DLG-10] Retrieval must score every KB section against the user
    message **in memory** (no database) and return, in descending score
    order, at most `RetrieveParams.top_k` sections whose score is at least
    `RetrieveParams.score_floor`, together with the single highest score seen.
    Scoring is BM25-lite: per distinct query term present in a section,
    `1 + ln(tf)` where a term in the section **title** counts
    `RetrieveParams.title_weight` times a term in the body, summed and divided
    by `ln(2 + body_token_count)` for length normalisation.
    -> [FUN-DLG-10] dialog.retrieve(query string, kb []Section) (hits []Section, topScore float64) — see plan.md pseudocode; deterministic: identical query+KB always yields identical ordering (stable sort, ties by section index)

16. [REQ-DLG-11] Before scoring, query and section tokens must be lowercased,
    stripped of punctuation, whitespace-split, and filtered against a fixed
    package-level stopword set (~40 Ukrainian + English function words). An
    empty query after filtering yields no hits and `topScore = 0`.
    -> [LOG-DLG-11] dialog stopwords var (map[string]bool); rationale: unfiltered stopwords let any two sections match on "the"/"of" — see docs/architecture.md §5 and ragline's tsquery lesson

17. [REQ-DLG-12] A turn must be classified as a **slot answer** when the user
    message is at most `RetrieveParams.slot_answer_max_tok` tokens **and** the
    session still has at least one nil quote slot — a deliberately loose
    heuristic so short replies like "5 pages" or "next week" are not treated
    as content questions.
    -> [FUN-DLG-12] dialog.isSlotAnswer(sess *Session, userText string) bool

18. [REQ-DLG-13] The grounding gate must decide, from `(topScore, slotAnswer)`
    only: a slot answer never escalates; otherwise a `topScore` below
    `RetrieveParams.score_floor` forces escalation **before** any Generator
    call; otherwise the turn proceeds. This is the sole anti-waffle mechanism
    — a content question the KB cannot answer never reaches the LLM.
    -> [FUN-DLG-13] dialog.groundingGate(topScore float64, slotAnswer bool) (forceEscalate bool) @constraint(rule: "pure function of its two arguments; no I/O, no session read")

19. [REQ-DLG-14] `dialog.Handle` must execute the exact 12-step sequence in
    plan.md: classify slot answer -> retrieve -> grounding gate (early return
    on escalate) -> build system prompt (`system` file + `--- KNOWLEDGE BASE
    ---` + retrieved bodies + `--- COLLECTED SO FAR ---` + compact slot JSON)
    -> append user msg, trim to `HistoryLimit` -> Generate -> parse trailer ->
    merge slots -> resolve signal -> append assistant msg -> return Reply.
    -> [FUN-DLG-14] dialog.Handle(ctx, sess *Session, kb []Section, gen Generator, systemPrompt, userText string) (Reply, error)

20. [REQ-DLG-15] Trailer parsing must take the **last** ```json fenced block
    in the model output, JSON-parse it into a Trailer, and validate
    `signal ∈ {continue, lead_ready, escalate}`. A missing block, a JSON
    error, or an unknown signal all yield `tr = nil`; the spoken text is the
    output with that block (and trailing whitespace) removed and trimmed.
    -> [FUN-DLG-15] dialog.parseTrailer(raw string) (spoken string, tr *trailer)

21. [REQ-DLG-16] Signal resolution: when `tr == nil` the turn escalates and is
    logged. Otherwise the signal is `tr.Signal`, except a `continue` is
    upgraded to `lead_ready` when `Session.Slots.Complete()` is true (all six
    non-nil). Any `escalate` (from the trailer or forced by the gate/parse
    failure) sets `Session.Escalated`.
    -> [LOG-DLG-16] dialog.Handle steps 8–10; QuoteSlots.Complete() gates the upgrade

22. [REQ-DLG-17] Any `Generator.Generate` error must be caught inside
    `dialog.Handle` and turned into a `Reply{Signal: escalate}` with a fixed
    apology line — the error is never returned to the caller and never crashes
    the update loop (see REQ-NFR-03).
    -> [LOG-DLG-17] dialog.Handle step 6: err path returns a Reply, nil error; apologyLine(lang) constant

23. [REQ-DLG-18] The assistant must never state a final total price; it
    collects parameters and defers the quote to a manager. Ranges quoted from
    the KB are allowed; a computed or committed total is not.
    -> [LOG-DLG-18] prompt/system.md "Hard rules"; kb/translation-bureau.md "How a price is formed" repeats it in-domain

24. [REQ-DLG-19] `langOf(text)` — used only to select the fixed handoff /
    apology line — returns `"uk"` if the text contains any Cyrillic rune, else
    `"en"`. The *conversation* language is the model's responsibility
    (REQ-DLG-05), not this function's.
    -> [FUN-DLG-19] dialog.langOf(text string) string

### 4.5 Knowledge base

25. [REQ-KB-01] The KB file must be loaded and split into titled sections on
    its `## ` headings; the assistant answers service questions only from
    these sections.
    -> [FUN-KB-01] kb.Load(path) ([]Section, error); Section{Title, Body}

### 4.6 Text-to-speech

26. [REQ-TTS-01] The bot must synthesize the reply text to OGG/Opus mono
    through a `Synthesizer` interface with two implementations —
    `elevenlabs` (`eleven_multilingual_v2`, default) and `azure`
    (`uk-UA-*Neural`) — selected by `tts_backend`.
    -> [FUN-TTS-01] tts.Synthesizer.Speak(ctx, text, voiceID, lang) ([]byte, error); impls tts.NewElevenLabs, tts.NewAzure chosen by Config.tts_backend

### 4.7 Session controls & first contact

27. [REQ-UX-01] `/voice a` and `/voice b` must switch, for that chat only,
    between the two configured voices; the choice persists on the session.
    -> [FUN-UX-01] cmd/bot update loop handles the /voice command by setting Session.voice; tts voiceID resolved from Config.{eleven,azure}_voice_{a,b}

28. [REQ-UX-02] The bot's first message in a chat (on `/start` or the first
    inbound message) must state that this is a demo and that the conversation
    is logged. It is fixed bilingual text, not LLM-generated.
    -> [FUN-UX-02] cmd/bot sends prompt/greeting.md verbatim once per chat, gated on Update.IsStart or an unseen chat id

### 4.8 Logging

29. [REQ-LOG-01] Every turn must be appended to a JSONL log as a TurnRecord
    (user text, reply text, signal, matched section titles, latency).
    -> [FUN-LOG-01] store.AppendTurn(dataDir, TurnRecord) after each dialog.Handle

---

## 5. Non-functional requirements

30. [REQ-NFR-01] Voice output must sound natural, not robotic — this is the
    single evaluation criterion. The two configured voices must read Ukrainian
    cleanly, including Latin surnames and EUR amounts. Azure Neural is the
    free fallback, a notch below ElevenLabs.
    -> [LOG-NFR-01] tts_backend default "elevenlabs" with model eleven_multilingual_v2; voice IDs chosen and checked per .engage/inventory.md I-4

31. [REQ-NFR-02] Voice-in to voice-out latency should target under ~10 s; the
    recording chat action (REQ-TG-04) must be shown while the chain runs.
    -> [LOG-NFR-02] no streaming; the chain is STT then one Generator call then one Speak call; SendRecordingAction precedes it

32. [REQ-NFR-03] Any single upstream failure (STT, Generator, Synthesizer,
    Telegram) must degrade to a text apology and the loop must keep serving
    other chats — never crash.
    -> [LOG-NFR-03] cmd/bot recovers a panic per handler goroutine; dialog/tts errors return a Reply/handoff text, not a process exit; see docs/architecture.md §8

33. [REQ-NFR-04] No real personal data anywhere: the KB is fictional; the only
    personal data in the logs is the client's own test messages.
    -> [LOG-NFR-04] kb/translation-bureau.md is invented ("FromToBridge"); no ingestion of external data; TurnRecord stores only what the tester typed

34. [REQ-NFR-05] Secrets must come only from the environment (or a local
    `.env`); never in code, logs, or git.
    -> [LOG-NFR-05] Config populated by cmd/bot.LoadConfig from env; .env is gitignored; log statements never print a full key

35. [REQ-NFR-06] All bot-authored copy must be in the user's language
    (Ukrainian by default); code, identifiers and comments in English.
    -> [LOG-NFR-06] prompt/system.md and prompt/greeting.md are UA/EN; Go source is English per S5

36. [REQ-NFR-07] The bot must be reachable by the client while they evaluate
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

None blocking implementation. Verification still pending: confirm Gemini Flash
on a Ukrainian dialogue test before it stays the default (fall back to
`gemma4:cloud` if it regresses).

**Resolved:** dialogue default Gemini Flash with `gemma4:cloud` / `gpt-4o-mini`
alternates; local Whisper with the `whisper-1` rollback; ElevenLabs Free
during build then Starter before the client link, Azure the free fallback;
fictional bureau "FromToBridge"; Ukrainian + English, no Russian; host local
for now.

# v2v-demo — requirements

Traced requirements for this throwaway demo. This is the *what* and *why*;
`docs/architecture.md` is the *shape*; `.agents/plan.md` is the *how*. Every
requirement cites a source key (S1–S5); the real references sit in `.engage/`
(local only — this repo is public).

## 1. Purpose & primary success criterion

A prospective translation-bureau client asked to **hear how the assistant
sounds and how it leads a dialogue**, on any topic, before committing to a
larger build. So the one criterion the demo is judged on is: *does the voice
sound natural and does the conversation feel unscripted*. Everything else
exists only to make that judgeable.

## 2. Sources (traceability keys)

Kept generic here; full references in `.engage/sources.md`.

| Key | Source |
|---|---|
| **S1** | the client's written brief — system description, the application questions, the prototype checklist, the GDPR section |
| **S2** | the pre-engagement chat — the client wants to *hear* the assistant, any topic is fine, agreed format is a Telegram bot with voice messages + a sample recording |
| **S3** | the agreed demo scope — neutral scenario, 2 voice options, a structured lead in Zoho-field shape (no real connection), a short fixed timeline, credited toward a later build; plus the explicit "not in the demo" list |
| **S4** | the chosen stack — Go, Telegram long-polling, local Whisper + Ollama `gemma4:cloud` / Gemini Flash + ElevenLabs multilingual v2, with config-flag alternates (`whisper-1`, `gpt-4o-mini`, Azure Neural). See `docs/architecture.md`. |
| **S5** | the project's coding conventions — Go for user-facing tools, GDPR awareness, never commit secrets, Ukrainian for user-facing copy |

## 3. Scope

In: a Telegram bot that holds a spoken quote-gathering conversation for a
fictional bureau, **in Ukrainian and English** (no RU — Q5), on fabricated
data, hosted so the client can poke at it, plus a short recorded sample
dialogue.

Out: everything in §6.

## 4. Functional requirements

| ID | Requirement | Source |
|---|---|---|
| **FR-1** | Accept a Telegram **voice message** from the user | S2, S3 |
| **FR-2** | Transcribe the voice message to text. Default: **local `whisper-cli`** (uk/en). Rollback: `whisper-1` API via `STT_BACKEND=openai` | S1 (natural voice interaction), S4, D-13 |
| **FR-3** | Accept a plain **text** message equivalently (STT skipped) — lets the client test without recording | S4 (pragmatic) |
| **FR-4** | Generate a natural, non-scripted reply from an LLM with the KB **and** rolling conversation history in context | S1 ("communicate naturally, without sounding like a scripted chatbot"; "understand context") |
| **FR-5** | Ask relevant clarifying questions that move toward a quote | S1 ("ask relevant follow-up questions"; prototype list) |
| **FR-6** | Answer questions about the fictional bureau's services from the KB | S1 ("answer questions about our services") |
| **FR-7** | Collect the six quote parameters: **language pair · document type · volume · deadline · certification/notarization · delivery** | S1 (explicit list) |
| **FR-8** | When all six are known: summarize them back, then emit a **structured lead record in Zoho-lead field shape** (written to the log, not sent anywhere) | S1 ("create/update leads in Zoho"); S3 (shown as structured lead, no real connection) |
| **FR-9** | Detect the user's language (**uk / en**) from the first turn, converse in it, follow a mid-dialogue switch. RU is out of demo scope (Q5) — if a RU message arrives the model may still answer, it is just not tested or prompted for | S1 ("Ukrainian, Russian and English" — the full system; the demo covers uk/en per Q5) |
| **FR-10** | Reply with a Telegram **voice message** (TTS) | S2 (client wants to hear it) |
| **FR-11** | `/voice a` \| `/voice b` — switch between two configured voices for comparison | S3 (2 voice options) |
| **FR-12** | Escalate to a human on: out-of-scope request, complaint, legal/liability or payment question, or explicit user request — reply with a fixed handoff line and mark the session escalated in the log | S1 ("escalate complex or non-standard requests"; "transfer to a human"); KB escalation list |
| **FR-13** | Append every turn to a log: user text, reply text, `answered`\|`escalated`\|`lead_ready`, matched KB topic, latency | S1 ("conversation history"; "analytics/logging"; "quality monitoring") — demo scale |
| **FR-14** | The bot's first message (on `/start` or first inbound) states it is a demo and that the conversation is logged — fixed text `prompt/greeting.md` (bilingual), not LLM-generated | S1 (GDPR section) — consent-awareness signal to the client |

## 5. Non-functional requirements

| ID | Requirement | Source |
|---|---|---|
| **NFR-1** | Voice output sounds natural, not robotic — **the** evaluation criterion. ElevenLabs `eleven_multilingual_v2` (primary); the two voices must read Ukrainian cleanly, incl. Latin names and EUR amounts. Azure Neural (`uk-UA-*`) is the free fallback, a notch below | S2, D-15 |
| **NFR-2** | Voice-in → voice-out latency target < ~10 s; send the "recording voice…" chat action while the chain runs | S1 (must be usable), S4 |
| **NFR-3** | The bot must be reachable by the client while they evaluate it. **For now it runs locally**; moving to an always-on host is a step before the client gets the link for an unattended window | S3 (the client drives it themselves, on their schedule) |
| **NFR-4** | Any single upstream failure (STT / LLM / TTS / Telegram) degrades to a text apology and the loop keeps serving — never crash | S4 |
| **NFR-5** | No real personal data anywhere — KB is fictional; the only personal data in logs is the client's own test messages | S1 (GDPR), S5 |
| **NFR-6** | Secrets only via env / `.env` (gitignored) — never in code, logs, or git | S5 |
| **NFR-7** | The assistant never states a final price — it collects parameters and defers the quote to a manager | S1 (pricing risk); KB |
| **NFR-8** | All bot-authored copy in the user's language (UA default); code / identifiers / comments in English | S5 |
| **NFR-9** | Answers are **grounded** in the KB: the assistant does not volunteer information the KB does not contain, and escalates instead of guessing when the KB does not cover the question. "Natural" means clear and coherent content, not just a natural voice — clarity and intelligibility, not gibberish | S2, S1 ("use our knowledge base to improve quality"; "not a scripted FAQ bot") |

## 6. Deferred — explicitly NOT in the demo

| ID | Deferred | Where it lands | Source |
|---|---|---|---|
| **D-1** | Real Zoho CRM connection (leads, contacts, history write-back; `.eu` vs `.com` base) | MVP | S3 |
| **D-2** | Telephony: real inbound calls, call recording, call transcription, warm transfer, phone number | MVP | S1; S3 (the client has no phone line to connect) |
| **D-3** | Real-time full-duplex voice (live-call feel, no record button) | MVP | S3 |
| **D-4** | Vector DB / real RAG retrieval — the demo does lightweight in-memory keyword retrieval only | MVP (ragivka) | S3, plan.md |
| **D-5** | Other channels: web widget, WhatsApp, etc. | MVP | S1 |
| **D-6** | Self-service knowledge-base editing without a redeploy | MVP | S1 |
| **D-7** | Persistence across restart, auth, multi-tenant, real analytics/monitoring dashboards | MVP | S1 |
| **D-8** | GDPR architecture — EU data residency, signed DPAs, retention/erasure policy, provider-side deletion. Demo shows only the consent line (FR-14) and uses no real data; the full design is an MVP deliverable and a proposal talking point | MVP + proposal | S1 (GDPR section) |

## 7. Inputs & materials

The list of accounts, keys, and authored assets the demo needs (I-1 … I-15),
with status, is in `.engage/inventory.md` (local only). Highlights: a throwaway
Telegram bot; ElevenLabs (Free → Starter); a fresh Gemini API key; `ffmpeg` +
`whisper.cpp` + a ggml model; a running Ollama. Authored and done:
`kb/translation-bureau.md`, `prompt/system.md`, `prompt/greeting.md`,
`examples/dialogues.md`.

## 8. Open questions

None blocking implementation. Verification still pending: confirm Gemini Flash
on a Ukrainian dialogue test before it stays the default (fall back to
`gemma4:cloud` if it regresses).

**Resolved:** Q1 → **Gemini Flash** dialogue default, alternates
`gemma4:cloud` / `gpt-4o-mini`; local Whisper, rollback `whisper-1` (D-13).
Q2 → host local for now (I-9). Q3 → ElevenLabs Free during build, Starter
before the client link; OpenAI keys fine for the rollback (D-15). Q4 →
"FromToBridge". Q5 → Ukrainian + English, no Russian.

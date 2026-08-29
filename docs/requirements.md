# ftb-demo — requirements

Traced requirements for the throwaway client demo. This is the *what* and
*why*; `docs/architecture.md` is the *shape*; `.agents/plan.md` is the *how*.
Every requirement cites a source.

Engagement-level source of truth (phases, decisions, commercial, asset map):
`<engagement-repo>/docs/00-overview.md`.
This file governs the demo only.

## 1. Purpose & primary success criterion

The prospective client (a translation bureau, a freelance engagement)
asked to **hear how the assistant sounds and how it leads a dialogue**, on any
topic, before committing to an MVP (S2). So the one criterion the demo is
judged on is: *does the voice sound natural and does the conversation feel
unscripted*. Everything else exists only to make that judgeable.

## 2. Sources (traceability keys)

| Key | Source |
|---|---|
| **S1** | `task.md` — client's full brief: system description, the 8 application questions, the "Preferred approach / prototype" list (`task.md:99-107`), and the GDPR section (`task.md:62-74`) |
| **S2** | `chat.md` — the pre-engagement chat: client 20260828T1547 (wants to hear sound + dialogue, not a past-work sample), client 20260828T1915 (any topic is fine), our reply 20260828T1930 (agreed format: Telegram bot, voice messages, + a sample recording) |
| **S3** | the agreed deal terms — commercial scope: neutral scenario, 2 voice options, structured lead in Zoho field shape (no real connection), a short fixed timeline, credited toward the MVP; and the explicit "not in the demo" list |
| **S4** | Val's stack decisions this session: Go, Telegram long-polling, OpenAI `whisper-1` + `gpt-4o-mini`, ElevenLabs multilingual v2 |
| **S5** | `~/.claude/CLAUDE.md` — Go for user-facing tools, GDPR awareness, never commit secrets, Ukrainian for client-facing copy |

## 3. Scope

In: a Telegram bot that holds a spoken quote-gathering conversation for a
fictional bureau, in UA/EN (RU if it appears), on fabricated data, hosted so
the client can poke at it for ~a week, plus a short recorded sample dialogue.

Out: everything in §6.

## 4. Functional requirements

| ID | Requirement | Source |
|---|---|---|
| **FR-1** | Accept a Telegram **voice message** from the user | S2, S3 |
| **FR-2** | Transcribe the voice message to text (STT, `whisper-1`) | S1 (natural voice interaction), S4 |
| **FR-3** | Accept a plain **text** message equivalently (STT skipped) — lets the client test without recording | S4 (pragmatic) |
| **FR-4** | Generate a natural, non-scripted reply from an LLM with the KB **and** rolling conversation history in context | S1 ("communicate naturally, without sounding like a scripted chatbot"; "understand context") |
| **FR-5** | Ask relevant clarifying questions that move toward a quote | S1 ("ask relevant follow-up questions"; prototype list) |
| **FR-6** | Answer questions about the fictional bureau's services from the KB | S1 ("answer questions about our services") |
| **FR-7** | Collect the six quote parameters: **language pair · document type · volume · deadline · certification/notarization · delivery** | S1 (explicit list) |
| **FR-8** | When all six are known: summarize them back, then emit a **structured lead record in Zoho-lead field shape** (written to the log, not sent anywhere) | S1 ("create/update leads in Zoho"); S3 (shown as structured lead, no real connection) |
| **FR-9** | Detect the user's language (uk / ru / en) from the first turn, converse in it, follow a mid-dialogue switch | S1 ("Ukrainian, Russian and English"; Q5 — context, not translated scripts) |
| **FR-10** | Reply with a Telegram **voice message** (TTS) | S2 (client wants to hear it) |
| **FR-11** | `/voice a` \| `/voice b` — switch between two configured voices for comparison | S3 (2 voice options) |
| **FR-12** | Escalate to a human on: out-of-scope request, complaint, legal/liability or payment question, or explicit user request — reply with a fixed handoff line and mark the session escalated in the log | S1 ("escalate complex or non-standard requests"; "transfer to a human"); KB escalation list |
| **FR-13** | Append every turn to a log: user text, reply text, `answered`\|`escalated`\|`lead_ready`, matched KB topic, latency | S1 ("conversation history"; "analytics/logging"; "quality monitoring") — demo scale |
| **FR-14** | The bot's first message states it is a demo and that messages are logged | S1 (GDPR section) — consent-awareness signal to the client |

## 5. Non-functional requirements

| ID | Requirement | Source |
|---|---|---|
| **NFR-1** | Voice output sounds natural, not robotic — **the** evaluation criterion. ElevenLabs multilingual v2; the two voices must read Ukrainian cleanly, including Latin names and EUR amounts | S2 |
| **NFR-2** | Voice-in → voice-out latency target < ~10 s; send the "recording voice…" chat action while the chain runs | S1 (must be usable), S4 |
| **NFR-3** | The bot must be reachable by the client while they evaluate it. **For now it runs locally** (Val, 2026-08-29); moving to an always-on host is a step before the client gets the link for an unattended window | S3 (client drives it themselves, on their schedule) |
| **NFR-4** | Any single upstream failure (STT / LLM / TTS / Telegram) degrades to a text apology and the loop keeps serving — never crash | S4 |
| **NFR-5** | No real personal data anywhere — KB is fictional; the only personal data in logs is the client's own test messages | S1 (GDPR), S5 |
| **NFR-6** | Secrets only via env / `.env` (gitignored) — never in code, logs, or git | S5 |
| **NFR-7** | The assistant never states a final price — it collects parameters and defers the quote to a manager | S1 (pricing risk); KB |
| **NFR-8** | All bot-authored copy in the user's language (UA default); code / identifiers / comments in English | S5 |
| **NFR-9** | Answers are **grounded** in the KB: the assistant does not volunteer information the KB does not contain, and escalates instead of guessing when the KB does not cover the question. "Natural" means clear and coherent content, not just a natural voice | S2 (Val's clarification — "внятність і природна зрозумілість… а не маячня"), S1 ("use our knowledge base to improve quality"; "not a scripted FAQ bot") |

## 6. Deferred — explicitly NOT in the demo

| ID | Deferred | Where it lands | Source |
|---|---|---|---|
| **D-1** | Real Zoho CRM connection (leads, contacts, history write-back; `.eu` vs `.com` base) | MVP | S3 |
| **D-2** | Telephony: real inbound calls, call recording, call transcription, warm transfer, phone number | MVP | S1; S3; chat (client has no number) |
| **D-3** | Real-time full-duplex voice (live-call feel, no record button) | MVP | S3 |
| **D-4** | Vector DB / real RAG retrieval — the demo puts the whole KB in the prompt | MVP (ragivka) | S3, plan.md |
| **D-5** | Other channels: web widget, WhatsApp, etc. | MVP | S1 |
| **D-6** | Self-service knowledge-base editing without a redeploy | MVP | S1 |
| **D-7** | Persistence across restart, auth, multi-tenant, real analytics/monitoring dashboards | MVP | S1 |
| **D-8** | GDPR architecture — EU data residency, signed DPAs, retention/erasure policy, provider-side deletion. Demo shows only the consent line (FR-14) and uses no real data; the full design is an MVP deliverable and a proposal talking point | MVP + proposal | S1 (GDPR section) |

## 7. Inputs & materials — what Val provides, and from where

| # | Item | What exactly | Where to get it | Status / cost |
|---|---|---|---|---|
| **I-1** | Telegram bot | a throwaway bot + token | Telegram → @BotFather → `/newbot` | Val creates · free |
| **I-2** | OpenAI API key | key with `whisper-1` + `gpt-4o-mini` | platform.openai.com → API keys | **confirm Val has one** · pay-as-you-go, demo ≈ $1–3 total |
| **I-3** | ElevenLabs API key | key with multilingual v2 | elevenlabs.io → Profile → API key | **confirm Val has one** · Free 10k chars/mo may cover it; Starter $5 |
| **I-4** | 2 voice IDs | two ElevenLabs voices that read Ukrainian cleanly — test each on a UA sentence containing a Latin surname and a EUR amount | ElevenLabs Voice Library → filter *multilingual* → preview | Val picks · free |
| **I-5** | Neutral KB | services, pairs, turnaround, indicative pricing, certified-vs-notarized, delivery, hours, escalation rules — all fictional | **written**: `kb/translation-bureau.md` (fictional "LinguaBridge") | done · Val may adjust numbers/tone |
| **I-6** | Handoff wording | the exact line the bot says on escalation (UA/RU/EN) | Claude drafts → Val approves tone | pending |
| **I-7** | Consent/demo line (FR-14) | first-message disclaimer text | Claude drafts | pending |
| **I-8** | System-prompt persona | assistant name, tone, do/don't rules, language policy | Claude drafts from KB + S1 → Val approves | pending |
| **I-9** | Host | run the long-polling bot | **local for now** (Val, 2026-08-29); an always-on host (Hetzner / Fly.io) is deferred until the client gets an unattended link | ≈ €0 now |
| **I-10** | Sample recorded dialogue | a 2–3 min realistic UA conversation with the deployed bot, exported as audio, to attach in `chat.md` | Val records himself talking to the bot | after the bot works |

## 8. Open questions

| # | Question | Note |
|---|---|---|
| **Q1** | Dialogue LLM: `gpt-4o-mini` (recommended — reliable multilingual dialogue, cheap) vs Ollama cloud (≈ free, quality risk on a client-facing demo) vs Claude Haiku | blocks I-2 |
| ~~Q2~~ | ~~Always-on host choice~~ — **resolved: local for now** (I-9) | |
| **Q3** | Does Val already hold the OpenAI (I-2) and ElevenLabs (I-3) keys, or create fresh? | |
| **Q4** | Keep the fictional bureau name "LinguaBridge" or rename? | cosmetic |
| **Q5** | RU in the demo — full uk/ru/en per FR-9, or UA primary + EN, RU only if it appears? (MVP tier was UA+EN, MMP was all three) | affects system-prompt + voice tests |

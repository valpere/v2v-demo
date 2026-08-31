# Manual smoke test

A scripted run through the demo. ~10 min. Use a real Telegram chat with the
bot. Not automated — the point is to hear the voice and judge the conversation.

## Setup

```bash
make run            # go run ./cmd/bot
```

- The **first voice message** downloads the Whisper model (~1.5 GB) — that
  turn takes minutes, once. Steady-state ≈ 12 s + LLM.
- The greeting is sent once per chat per process. To re-trigger it: `/start`,
  or restart the bot, or use a fresh chat.
- Watch `data/turns.jsonl` / `data/leads.jsonl` as you go
  (`tail -f data/turns.jsonl | jq .`).

---

## 1. First contact

| Send | Expect |
|--|--|
| `/start` | The bilingual greeting (UK block, then EN block). Nothing else. |
| (any first message in a fresh chat) | Greeting once, then the message is answered. |

## 2. Text quote flow — reaches `lead_ready`

Send these in order (text — no STT), one per line:

1. `Треба перекласти диплом з української на німецьку` → asks for 1–2 missing
   things, not all six at once.
2. `2 сторінки`
3. `за тиждень`
4. `для університету в Берліні` → should settle `certification` from the KB
   (bureau stamp is enough for a university) without asking "certified or
   notarized?".
5. `скан на пошту`

**Expect on the last turn:** a short read-back of all six values, "менеджер
надішле вартість protягом ~15 хв", **no final total**, `signal: lead_ready`.
**Check:** one new row in `data/leads.jsonl` with those six fields; no
fabricated values (e.g. it must not invent a page count you didn't give).

## 3. Voice in / out

6. Send a **short Ukrainian voice message**, e.g. "скільки коштує переклад
   паспорта". → "recording voice" action stays visible; you get a voice note
   **and** the same text; the reply is grounded in the KB.
7. Send an **unintelligible** voice (cough / silence) → "Не розчув(ла),
   повторіть" — and **no** new row in `data/turns.jsonl`.

## 4. `/voice`

8. `/voice b` → "гаразд, тепер інший голос"; the next voice reply is the
   second voice (George).
9. `/voice` (no arg) → a one-line usage hint, **not** a dialogue turn.
10. `/wat` → same hint, not a dialogue turn.

## 5. Escalation (fast — no LLM call)

11. `яка зараз погода в Києві?` → the fixed handoff line
    (grounding gate: off-topic, low KB overlap).
12. `поверніть мені гроші` → handoff (hardEscalate keyword).
13. `дякую, до побачення` → a normal polite close, **not** a handoff
    (small-talk bypasses the gate).

## 6. Language

14. In a fresh chat, open in **English**: `How much for a certified contract
    translation, about 20 pages?` → replies in English, mentions the higher
    band for legal texts, no final total.
15. Mid-conversation, switch: `Продовжимо українською. А нотаріальне
    засвідчення?` → switches to Ukrainian.
16. Speak Ukrainian in a voice message but sloppily → transcript should stay
    Ukrainian (not drift to Russian), reply in Ukrainian.

---

## What "pass" looks like

- Voice sounds natural on Ukrainian, including Latin surnames and EUR amounts
  (the single evaluation criterion — NFR-1).
- The assistant asks only for what it doesn't know, never quotes a final
  price, reads back before `lead_ready`, and hands off cleanly on the §5 cases.
- `data/turns.jsonl` has every turn (`signal`, `matched`, `slots`,
  `latency_ms`); `data/leads.jsonl` has exactly the completed quotes, with
  only values the client actually gave.
- Nothing crashes the bot — a failed STT/LLM/TTS call degrades to text.

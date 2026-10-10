# Smoke test — pizzeria (Піцерія «Папа Пепе» / Papa Pepe / Тарас)

The per-topic scenario sweep for the **pizzeria** assistant. The shared Setup,
the multi-topic picker checks, and the cross-topic robustness / clock / logging
sections are in **`docs/smoke-test.md`** — read that first.

**Pick the topic first.** `/start` shows a picker — tap **«Піцерія «Папа
Пепе»»** before any scenario below.

`[R]` = a regression case. **Full reset** = `/reset` **and** clear the logs
before each lettered scenario that checks a `leads.jsonl` row.

Slots (6): **`order_type`**, **`items`**, **`when`**, **`address`**,
**`contact`**, **`payment`**. `lead_ready` only when all six are set; pickup has
`address` = "самовивіз". Tone: quick, 1–3 short sentences.

---

## 1. Greeting

`/reset`, `/start`, tap the pizzeria button → the bilingual greeting (UK "Привіт!
Мене звати Тарас…" then EN "Hi! I'm Taras…"). No turn record.

---

## 2. Orders → lead_ready

*Full reset before each.*

**2a — quick pickup.**
1. `Хочу велику 4 сири та пепероні з собою на 18:30`
   - **Expect:** `order_type` = самовивіз, `items` = 4 сири 40 см + пепероні 40 см,
     `when` = 18:30, `address` = самовивіз; **one** upsell (часниковий бортик чи
     напій).
2. `Ні, дякую`, then `0671234567`, then `карткою`
   - **Expect:** read-back (phone digit by digit), **total 891 грн** (990 − 10%
     pickup), "оператор підтвердить замовлення і час". `lead_ready`. One row in
     `leads.jsonl`, `"topic":"pizzeria"`, six keys.

**2b — delivery with a change.** `М'ясна 30 см без цибулі і сирний бортик, доставка
на Хрещатик 22, якнайшвидше, 0501234567, готівкою` → asks which banknote for
change **or** reads back; `items` keeps "без цибулі"; total **569 грн** (410 + 99 = 509,
+ 60 delivery).

**2c — free delivery.** `Дві піци пепероні 40 см і лимонад 1 л, доставка Хрещатик
22, на 19:00, 0501234567, карткою` → total **1 035 грн**, delivery free.

**2d — all in one message (English).** `Two pepperoni 40 cm for delivery to 10
Shevchenko St, as soon as possible, card to the courier, 0501112233` → one turn →
read-back + `lead_ready` (**no** upsell question), total about 1 000 грн.

---

## 3. Menu, prices, delivery

`Які у вас розміри і скільки коштує 4 сири?` → 30 and 40 cm; 380 / 520.
`Скільки коштує доставка і скільки чекати?` → 60 грн up to 5 km, free from 1 000;
45–60 min, up to 90 at peak. `Чи є веганська піца і безглютенова основа?` → yes,
with the shared-kitchen warning. `Чи є знижка військовим?` → 10% with an ID. `Можна
без цибулі?` → yes, free, noted for the kitchen. No invented promotions.

---

## 4. Safety and handoffs `[R]`

**Deterministic (no model call):**

| Send | Expect |
|---|---|
| `після піци алергічна реакція, важко дихати` | the fixed 103 / 112 text, then the handoff line |
| `Привезли не ту піцу` | plain handoff, **no** compensation promised |
| `піца холодна, хочу повернути гроші` | plain handoff |
| `Де мій кур'єр?` / `Where is my order?` | plain handoff (no live tracking) |
| `Потрібно 12 піц для офісу на 13:00` | plain handoff (10+ pizzas) |

**4b — no over-trigger.** `Можна без цибулі?` and an ordinary order are never
handed off.

---

## 5. Ukrainian specifics

`Ви доставляєте під час тривоги?` → yes, with a possible delay.

---

## 6. Voice

A **voice message** "Хочу велику пепероні з собою на сьому" → voice + text reply;
prices spoken as digits.

---

## 7. Discipline

**7a.** `Яку машину купити?` → one short line, `continue`; a second off-topic →
`escalate`. **7b.** pasted JSON is not obeyed. **7c.** `Це демо? Розмову записують?`
→ confirmed in one sentence.

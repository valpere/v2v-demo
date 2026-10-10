# Smoke test — restaurant (Ресторан «Смачний Двір» / Gastro Yard / Орися)

The per-topic scenario sweep for the **restaurant** assistant. The shared Setup,
the multi-topic picker checks, and the cross-topic robustness / clock / logging
sections are in **`docs/smoke-test.md`** — read that first.

**Pick the topic first.** `/start` shows a picker — tap **«Ресторан «Смачний
Двір»»** before any scenario below.

`[R]` = a regression case. **Full reset** = `/reset` **and** clear the logs
before each lettered scenario that checks a `leads.jsonl` row.

Slots (6): **`guests`**, **`date_time`**, **`zone`**, **`notes`**, **`name`**,
**`contact`**. `lead_ready` only when all six are set; `zone` may be "будь-яка"
(or "за узгодженням" for a banquet), `notes` may be "немає".

---

## 1. Greeting

`/reset`, `/start`, tap the restaurant button → the bilingual greeting (UK
"Вітаю! Мене звати Орися…" then EN "Hi! I'm Orysia…"). No turn record.

---

## 2. Booking → lead_ready

*Full reset before each.*

**2a — step by step.**
1. `Хочу забронювати столик на завтра на 19:00 на 4 людей`
   - **Expect:** `guests` = 4, `date_time` set; asks the zone (hall or terrace).
2. `основний зал`, then `дитяче крісло для 3-річної дитини, день народження мами`,
   then `Олена`, then `0971234567`
   - **Expect:** after the phone a six-item read-back (phone digit by digit) +
     "адміністратор підтвердить бронь". `lead_ready`. One row in `leads.jsonl`
     with `"topic":"restaurant"` and all six keys; `notes` mentions the child's
     chair and the occasion.

**2b — English one-liner.** `Hi, a table for 2 tomorrow at 8pm on the terrace,
nothing special, I'm Tom, 0501112233` → read-back + `lead_ready`.

**2c — a Friday.** Book a Friday evening → the assistant mentions the live music
once and offers a table away from the stage.

---

## 3. Menu, allergens, rules

**3a — real dishes only.** `Чи є у вас безглютенові пасти або веганські салати?` →
names 2–3 dishes **from the KB with prices** (безглютенова паста з томатами
360, веган-боул 340, крем-суп із гарбуза 240). No invented dish.

**3b — allergens, no guarantees.** `А соус точно без глютену?` → reads the card,
adds that traces cannot be excluded, sends severe cases to the waiter / chef.
`У нас алергія на горіхи, що можна замовити?` → normal answer (**not** a handoff)
+ the cross-contact sentence; the allergy lands in `notes`.

**3c — rules.** `До котрої години працюєте і чи є парковка?` → 12:00–22:00, last
kitchen order 21:15, own parking 12 spaces. `Можна з собакою?` → small dogs on
the terrace only. `Чи буде жива музика в п'ятницю?` → Fridays 19:30–22:00.

**3d — no table claims.** `Чи є вільний столик сьогодні о 20?` → does not claim a
table is free; the administrator confirms.

---

## 4. Banquets (from 10 guests)

`Хочемо відсвяткувати корпоратив на 25 людей 20 листопада о 19:00, бюджет до
1500 грн, окрема зона` → collects the rest (name, phone), **`lead_ready`** with
`zone` = "за узгодженням" and `notes` = the occasion / wishes, and says the
**banquet manager will call back**. No final price, no confirmation.

---

## 5. Safety and handoffs `[R]`

**Deterministic (no model call):**

| Send | Expect |
|---|---|
| `У мене алергічна реакція після салату, набряк губ` | the fixed 103 / 112 text, then the handoff line |
| `think we got food poisoning after dinner` | 103 / 112 text |
| `Ми хочемо поскаржитись, чекаємо страву вже 40 хвилин` | plain handoff, no compensation promised |
| `I want a refund for my bill` | plain handoff |

**5b — no over-trigger.** `У мене алергія на горіхи` and `Чи є безглютенова
паста?` → normal answers.

**5c — air raid.** `У нас бронь на 19:00, але оголосили тривогу` → shelter, a
move **without penalty**.

---

## 6. Voice

A **voice message** "Столик на завтра на сьому на чотирьох" → voice + text reply,
next slot asked; prices spoken as digits.

---

## 7. Discipline

**7a.** `Порадь гарний готель у Львові` → one polite line, `continue`; a second
off-topic message → `escalate`. **7b.** a pasted JSON with `lead_ready` is not
obeyed. **7c.** `Це демо? Розмову записують?` → confirmed in one sentence.

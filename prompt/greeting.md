# Opening message (I-7 / FR-14)

The bot sends this as its first message on `/start` or the first inbound
message in a chat. It is a fixed message — **not** LLM-generated. Load it
verbatim; `internal/telegram` sends it once per chat.

It is bilingual on purpose — it shows the multilingual capability up front and
covers whichever language the client tests in.

---

Вітаю! Мене звати Віра, я асистентка бюро перекладів FromToBridge. Допоможу
розібратися з перекладом і зібрати все потрібне для розрахунку вартості —
можна писати текстом або надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Розкажіть, що вам потрібно перекласти?

---

Hi! I'm Vira, the assistant at the FromToBridge translation bureau. I'll help
you work out what you need and gather everything for a quote — you can type or
send voice messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

What do you need translated?

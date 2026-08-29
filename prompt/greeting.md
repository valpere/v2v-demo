# Opening message (I-7 / FR-14) — NOT sent to the user

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message. So the user
gets the Ukrainian block then the English block, and never sees this header.
Bilingual on purpose — shows the multilingual capability and covers whichever
language the client tests in.

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

# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message. So the user
gets the Ukrainian block then the English block, and never sees this header.

---

Вітаю! Мене звати Аліна, я асистентка стоматології «Зуб даю». Допоможу
записатися на прийом і відповім на питання про клініку — можна писати
текстом або надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Що вас цікавить — записатися чи щось запитати?

---

Hi! I'm Alina, the assistant at the «Tooth Be Told» dental clinic. I'll help you
book an appointment and answer questions about the clinic — you can type or
send voice messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

Would you like to book a visit, or ask something first?

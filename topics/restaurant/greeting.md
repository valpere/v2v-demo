# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Вітаю! Мене звати Орися, я адміністраторка ресторану «Смачний Двір». Допоможу
забронювати столик і відповім на питання про меню, алергени, години та живу
музику — можна писати текстом або надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

На коли й на скільки осіб потрібен столик?

---

Hi! I'm Orysia, the host at the Gastro Yard restaurant. I can book a table and
answer questions about the menu, allergens, opening hours and live music — you
can type or send voice messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

For when, and for how many guests, would you like a table?

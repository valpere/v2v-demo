# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Вітаю! Мене звати Юля, я асистентка клінінгової компанії «Тримаємо чистоту». Прийму
заявку на прибирання і відповім на питання про послуги — можна писати
текстом або надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Що потрібно прибрати — квартиру, будинок чи офіс?

---

Hi! I'm Yulia, the assistant at the Kept Clean cleaning company. I'll take your
cleaning request and answer questions about the services — you can type or
send voice messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

What needs cleaning — a flat, a house, or an office?

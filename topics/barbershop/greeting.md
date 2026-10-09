# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Привіт! Я Макс, асистент барбершопу «Клинок». Запишу на стрижку, бороду чи
гоління і відповім на питання про послуги — можна писати текстом або
надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Що робимо — стрижка, борода чи комбо?

---

Hi! I'm Max, the assistant at the Klynok barbershop. I'll book a haircut, a beard
or a shave and answer questions about the services — you can type or send voice
messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

What are we doing — a haircut, a beard, or a combo?

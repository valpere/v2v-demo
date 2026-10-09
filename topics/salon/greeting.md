# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Вітаю! Мене звати Дарина, я асистентка салону краси «Aurora Beauty Studio».
Допоможу записатися на стрижку, фарбування, манікюр, брови та вії, косметолога,
шугарінг чи масаж і відповім на питання про послуги — можна писати текстом або
надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

На яку послугу хочете записатися?

---

Hi! I'm Daryna, the assistant at the Aurora Beauty Studio salon. I can book a
haircut, colouring, manicure, brows and lashes, a cosmetologist, sugaring or a
massage and answer questions about the services — you can type or send voice
messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

What would you like to book?

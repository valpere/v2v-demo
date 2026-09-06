# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Вітаю! Мене звати Оксана, я асистентка агенції нерухомості «Ваші Ключі». Допоможу
підібрати квартиру чи будинок під ваші критерії і відповім на питання про
роботу агенції — можна писати текстом або надсилати голосові, українською чи
англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Що ви шукаєте — купівлю чи оренду, і яку саме нерухомість?

---

Hi! I'm Oksana, the assistant at the Your Keys real-estate agency. I'll help
you find a flat or a house matching your criteria and answer questions about
how the agency works — you can type or send voice messages, in Ukrainian or
English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

Are you looking to buy or to rent, and what kind of property?

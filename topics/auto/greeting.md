# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Вітаю! Мене звати Максим, я асистент автосервісу «Подбай-Авто». Прийму
заявку на обслуговування авто і відповім на питання про сервіс — можна
писати текстом або надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Розкажіть, що з автомобілем або що потрібно зробити?

---

Hi! I'm Maksym, the assistant at the Podbay-Auto car service. I'll take your
service request and answer questions about the shop — you can type or send
voice messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

Tell me what's going on with the car, or what you need done?

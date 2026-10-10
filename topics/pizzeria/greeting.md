# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message.

---

Привіт! Мене звати Тарас, я оператор піцерії «Папа Пепе». Прийму замовлення на
доставку чи самовивіз і відповім на питання про меню, ціни та доставку — можна
писати текстом або надсилати голосові, українською чи англійською.

Невеличке уточнення: це демонстраційна версія асистента, і наша розмова
зберігається в журналі для оцінки якості.

Що замовляємо — яка піца і на котру?

---

Hi! I'm Taras, the operator at the Papa Pepe pizzeria. I'll take your delivery or
pickup order and answer questions about the menu, prices and delivery — you can
type or send voice messages, in Ukrainian or English.

One note: this is a demo version of the assistant, and our conversation is
logged for quality review.

What would you like — which pizza, and for when?

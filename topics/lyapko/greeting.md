# Opening message — NOT sent to the user as-is

The bot sends this on `/start` or the first inbound message in a chat, once
per chat. Fixed text, not LLM-generated. **Load rule:** the message is the
file content after the first line that is exactly `---`; drop any further
lines that are exactly `---`; trim; send as **one** message. So the user
gets the Ukrainian block then the English block, and never sees this header.

---

Вітаю! Мене звати Олена, я демо-консультантка за мотивами магазину «Lyapko Shop». Допоможу підібрати ідеальний аплікатор Ляпка під вашу задачу (біль у спині чи шиї, безсоння, омолодження або зняття стресу), визначитися з кроком голок та відповім на будь-які питання. Можна писати текстом або надсилати голосові повідомлення, українською чи англійською.

Це неофіційна демонстраційна версія асистента, не пов'язана з магазином «Lyapko Shop»; наша розмова зберігається в журналі для оцінки якості.

Що вас турбує або який масажер шукаєте?

---

Hi! I'm Elena, a demo product advisor modelled on «Lyapko Shop». I'll help you find the right Lyapko acupressure applicator for your needs (back or neck pain, sciatica, sleep quality, facial care, or stress relief), pick the ideal needle pitch, and answer any product questions. You can type or send voice notes, in English or Ukrainian.

Note: this is an unofficial demo assistant, not affiliated with «Lyapko Shop»; our conversation is logged for quality evaluation.

What symptom or wellness goal brings you here today?

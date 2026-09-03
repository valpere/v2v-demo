package tts

import "testing"

func TestSpoken(t *testing.T) {
	cases := []struct{ in, lang, want string }{
		{"plain text stays put", "uk", "plain text stays put"},
		{"General text, UA ⇄ EN / DE / PL", "uk", "General text, UA — EN / DE / PL"},
		{"**Записала:** диплом", "uk", "Записала: диплом"},
		{"вартість `12` євро", "uk", "вартість 12 євро"},
		{"- перший\n- другий", "uk", "перший\nдругий"},
		{"## Підсумок\nтекст", "uk", "Підсумок\nтекст"},
		{"uk→en switch", "uk", "uk — en switch"},
		{"a  b   c", "uk", "a b c"},
		{"  padded  ", "uk", "padded"},

		{"12–16 EUR за сторінку", "uk", "12–16 євро за сторінку"},
		{"about 12–16 EUR per page", "en", "about 12–16 euro per page"},
		{"безкоштовно понад EUR 150", "uk", "безкоштовно понад 150 євро"},
		{"free over EUR 150", "en", "free over 150 euro"},
		{"€40 за документ", "uk", "40 євро за документ"},
		{"pay in USD", "en", "pay in dollars"},
		{"оплата в USD", "uk", "оплата в доларів"},

		{"переказом із ПДВ або без", "uk", "переказом із пе де ве або без"},
		{"a signed NDA on request", "en", "a signed ен ді ей on request"},
		{"працює 09:00–18:00 EET", "uk", "працює 09:00–18:00 за київським часом"},
		{"надішліть ЄДРПОУ", "uk", "надішліть є де ер пе о у"},
		{"доставка кур'єром DHL за кордон", "uk", "доставка кур'єром де ха ел за кордон"},
		{"оплата через Privat24 або картку", "uk", "оплата через Приват24 або картку"},
		{"pay via Privat 24", "en", "pay via Приват24"},
	}
	for _, c := range cases {
		if got := Spoken(c.in, c.lang); got != c.want {
			t.Errorf("Spoken(%q, %q) = %q, want %q", c.in, c.lang, got, c.want)
		}
	}
}

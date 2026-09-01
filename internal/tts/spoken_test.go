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
	}
	for _, c := range cases {
		if got := Spoken(c.in, c.lang); got != c.want {
			t.Errorf("Spoken(%q, %q) = %q, want %q", c.in, c.lang, got, c.want)
		}
	}
}

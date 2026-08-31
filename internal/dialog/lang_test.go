package dialog

import "testing"

func TestDetectLang(t *testing.T) {
	uk := []string{
		"Привіт!",
		"Доброго дня ще раз",
		"Мені треба перекласти диплом з української на німецьку",
		"Дякую. Ви були цілком люб'язні.",
		"Добрый день еще раз",                   // Russian -> uk (out of scope)
		"Мне очень интересно на вашей послуги.", // Russian -> uk
	}
	en := []string{
		"Hello there",
		"I need to translate a contract from Ukrainian to English",
		"How much for a certified translation of a diploma?",
	}
	blank := []string{"", "   ", "123 456"}

	for _, s := range uk {
		if got := detectLang(s); got != "uk" {
			t.Errorf("detectLang(%q) = %q, want uk", s, got)
		}
	}
	for _, s := range en {
		if got := detectLang(s); got != "en" {
			t.Errorf("detectLang(%q) = %q, want en", s, got)
		}
	}
	for _, s := range blank {
		if got := detectLang(s); got != "" {
			t.Errorf("detectLang(%q) = %q, want \"\"", s, got)
		}
	}
}

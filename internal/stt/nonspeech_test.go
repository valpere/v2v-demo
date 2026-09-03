package stt

import "testing"

func TestIsNonSpeech(t *testing.T) {
	nonSpeech := []string{
		"", "   ", "...", "!",
		"Дякую за перегляд!", "дякую за перегляд", "Дякую за увагу.",
		"Thanks for watching.", "Thank you for watching",
		"Продовження буде",
	}
	speech := []string{
		"Дякую", "Дякую, до побачення", "Переклад диплома",
		"Три сторінки", "Thanks, that's all I needed",
	}
	for _, s := range nonSpeech {
		if !IsNonSpeech(s) {
			t.Errorf("IsNonSpeech(%q) = false, want true", s)
		}
	}
	for _, s := range speech {
		if IsNonSpeech(s) {
			t.Errorf("IsNonSpeech(%q) = true, want false", s)
		}
	}
}

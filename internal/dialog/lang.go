package dialog

import (
	"strings"

	"github.com/pemistahl/lingua-go"
)

// langDetector is restricted to the languages the demo cares about. Russian
// is included so a Russian message (out of scope, or an STT mis-hear of
// Ukrainian) is recognised — and then treated as Ukrainian.
var langDetector = lingua.NewLanguageDetectorBuilder().
	FromLanguages(lingua.Ukrainian, lingua.English, lingua.Russian).
	WithPreloadedLanguageModels().
	Build()

// detectLang classifies text as "uk" | "en" | "" ("" = not enough signal).
// Russian maps to "uk": RU is out of the demo's scope, and Whisper sometimes
// transcribes Ukrainian speech as Russian.
func detectLang(text string) string {
	if strings.TrimSpace(text) == "" {
		return ""
	}
	switch lang, ok := langDetector.DetectLanguageOf(text); {
	case !ok:
		return ""
	case lang == lingua.English:
		return "en"
	case lang == lingua.Ukrainian, lang == lingua.Russian:
		return "uk"
	default:
		return ""
	}
}

// sessLang is Session.Lang if set, else "uk" — used only to pick the fixed
// handoff / apology / stt-fail / clarify / voice-switch lines. The bureau is
// Kyiv-based and the KB is Ukrainian-first, so an undetected language (a
// cough on the first turn, "ok") defaults to Ukrainian, not English.
func sessLang(sess *Session) string {
	if sess.Lang != "" {
		return sess.Lang
	}
	return "uk"
}

func langName(code string) string {
	if code == "uk" {
		return "Ukrainian"
	}
	return "English"
}

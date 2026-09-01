package tts

import (
	"regexp"
	"strings"
)

var (
	reArrow   = regexp.MustCompile(`\s*(?:<->|<=>|<-|->|=>|⇄|⇆|↔|→|←)\s*`)
	reBullet  = regexp.MustCompile(`(?m)^[ \t]*[-*•·]+[ \t]+`)
	reHeading = regexp.MustCompile(`(?m)^[ \t]*#{1,6}[ \t]+`)
	reEmph    = regexp.MustCompile("[*`]+")
	reSpace   = regexp.MustCompile(`[ \t]{2,}`)

	// currency codes the TTS engine otherwise spells out letter by letter.
	// Symbols are normalised to codes first (with padding, so \b holds).
	reCurBefore = regexp.MustCompile(`(?i)\b(EUR|USD)\b\s*(\d[\d.,]*)`)
	reCurAfter  = regexp.MustCompile(`(?i)(\d[\d.,]*)\s*\b(EUR|USD)\b`)
	reCurBare   = regexp.MustCompile(`(?i)\b(EUR|USD)\b`)
)

// currencyWord maps a code to its spoken form in the reply language
// ("uk" | "en" | "").
func currencyWord(code, lang string) string {
	usd := strings.EqualFold(code, "USD")
	switch {
	case lang == "en" && usd:
		return "dollars"
	case lang == "en":
		return "euro"
	case usd:
		return "доларів"
	default:
		return "євро"
	}
}

// Spoken strips markup a TTS engine would read aloud as noise before it ever
// reaches Speak — markdown emphasis and list / heading markers, the KB's
// directional-arrow shorthand ("UA ⇄ EN"), and currency codes it spells out
// ("12–16 EUR" → "12–16 євро"). It is a safety net: prompt/system.md already
// tells the model to phrase replies for the ear; this catches the stray "**",
// a copied arrow, or an "EUR" that slips through. The text message keeps the
// original wording.
func Spoken(s, lang string) string {
	s = reArrow.ReplaceAllString(s, " — ")
	s = reBullet.ReplaceAllString(s, "")
	s = reHeading.ReplaceAllString(s, "")
	s = reEmph.ReplaceAllString(s, "")

	s = strings.ReplaceAll(s, "€", " EUR ")
	s = strings.ReplaceAll(s, "$", " USD ")
	s = reCurBefore.ReplaceAllStringFunc(s, func(m string) string {
		g := reCurBefore.FindStringSubmatch(m)
		return g[2] + " " + currencyWord(g[1], lang)
	})
	s = reCurAfter.ReplaceAllStringFunc(s, func(m string) string {
		g := reCurAfter.FindStringSubmatch(m)
		return g[1] + " " + currencyWord(g[2], lang)
	})
	s = reCurBare.ReplaceAllStringFunc(s, func(m string) string {
		return currencyWord(m, lang)
	})

	s = reSpace.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

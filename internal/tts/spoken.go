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

	// a trailing zero-only fractional part ("12.00", "12,00") never needs
	// speaking — drop it before the % / м² passes below so "12.00%" becomes
	// "12%" first. Run before reBrand too: harmless either way since
	// "Приват24" has no decimal point, but this keeps the "digit cleanup
	// first" ordering consistent.
	reTrailingZeroFrac = regexp.MustCompile(`(\d)[.,]0+(\D|$)`)

	// percent sign — bare, after a number or a dash-range of numbers. Also
	// matches the U+2212 MINUS some KB entries use for a discount ("−15%").
	rePercent = regexp.MustCompile(`%`)

	// м²/m² (or the plain м2/m2 fallback, no superscript) — Cyrillic м in
	// Ukrainian KB text, Latin m in English text, same U+00B2 superscript.
	// \b is ASCII-only in RE2 (see reABBRcyr above) so a Cyrillic м needs a
	// \p{L} boundary, not \b, or it silently fails to match at all.
	//
	// Two grammatical shapes share this symbol and must NOT get the same
	// word: a rate ("за м²" / "per m²", singular — "за квадратний метр") vs
	// an area quantity ("55 м²", plural genitive — "квадратних метрів").
	// reSqMetersRate is the more specific pattern and must run first.
	reSqMetersRate = regexp.MustCompile(`(?i)(^|[^\p{L}])(за|per)(\s+)([мm])[²2]($|[^\p{L}\d])`)
	reSqMeters     = regexp.MustCompile(`(?i)(^|[^\p{L}])([мm])[²2]($|[^\p{L}\d])`)

	// abbreviations the neural voice mangles ("ПДВ" -> "Проблем Дальнього
	// Востока" on Azure uk-UA). Replaced with the spelled-out letter names.
	// \b is ASCII-only in RE2, so the Cyrillic set uses \p{L} boundaries.
	reABBRcyr = regexp.MustCompile(`(^|[^\p{L}])(ЄДРПОУ|ЄДРПО|ПДВ|НДА)($|[^\p{L}])`)
	reABBRlat = regexp.MustCompile(`\b(NDA|EET|DHL)\b`)

	// Latin brand names the uk-UA voice mispronounces reading them as plain
	// Latin letters. Give each its correct Ukrainian phonetic form —
	// language-agnostic (unlike currency/%/м², these stay Cyrillic even in
	// an English reply, same as a proper noun keeps its own pronunciation
	// mid-sentence in any language). Privat24: English stress ("PRIvat" not
	// "приВАТ"). Skoda: reads as "Скода" (plain transliteration) instead of
	// the correct "Шкода" (found live, 2026-09-17, auto topic §0d).
	reBrand = regexp.MustCompile(`(?i)\bPrivat\s?24\b|\bSkoda\b`)
)

var brandSpoken = map[string]string{
	"privat24": "Приват24",
	"skoda":    "Шкода",
}

var abbrSpoken = map[string]string{
	"ПДВ":    "пе де ве",
	"НДА":    "ен ді ей",
	"NDA":    "ен ді ей",
	"ЄДРПОУ": "є де ер пе о у",
	"ЄДРПО":  "є де ер пе о",
	"EET":    "за київським часом",
	"DHL":    "ді ейч ель", // uk voice reads "DHL" as a word ("дихаель")
}

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

// percentWord is the spoken form of "%" in the reply language.
func percentWord(lang string) string {
	if lang == "en" {
		return "percent"
	}
	return "відсотків"
}

// sqMetersWord is the spoken form of a bare area quantity ("55 м²") in the
// reply language — plural genitive in Ukrainian.
func sqMetersWord(lang string) string {
	if lang == "en" {
		return "square meters"
	}
	return "квадратних метрів"
}

// sqMetersRateWord is the spoken form of a per-unit rate ("за м²" / "per
// m²") — singular, "квадратний метр" not "квадратних метрів".
func sqMetersRateWord(lang string) string {
	if lang == "en" {
		return "square meter"
	}
	return "квадратний метр"
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

	s = reABBRcyr.ReplaceAllStringFunc(s, func(m string) string {
		g := reABBRcyr.FindStringSubmatch(m)
		return g[1] + abbrSpoken[g[2]] + g[3]
	})
	s = reABBRlat.ReplaceAllStringFunc(s, func(m string) string {
		return abbrSpoken[m]
	})
	s = reBrand.ReplaceAllStringFunc(s, func(m string) string {
		key := strings.ToLower(strings.Join(strings.Fields(m), ""))
		return brandSpoken[key]
	})

	// digit cleanup first: drop a trailing zero-only fraction before any
	// symbol word-substitution runs, so "12.00%" becomes "12%" before the
	// percent pass below.
	s = reTrailingZeroFrac.ReplaceAllString(s, "$1$2")

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

	s = reSqMetersRate.ReplaceAllStringFunc(s, func(m string) string {
		g := reSqMetersRate.FindStringSubmatch(m)
		return g[1] + g[2] + g[3] + sqMetersRateWord(lang) + g[5]
	})
	s = reSqMeters.ReplaceAllStringFunc(s, func(m string) string {
		g := reSqMeters.FindStringSubmatch(m)
		return g[1] + sqMetersWord(lang) + g[3]
	})
	s = rePercent.ReplaceAllString(s, " "+percentWord(lang))

	s = reSpace.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimRight(ln, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

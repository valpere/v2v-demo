package dialog

import (
	"math"
	"testing"

	"github.com/valpere/v2v-demo/internal/kb"
)

func testKB() []kb.Section {
	return []kb.Section{
		{Title: "Services", Body: "Written translation, certified translation, notarized translation, sworn translation for German Polish Italian French Czech."},
		{Title: "Delivery", Body: "Scanned PDF by email, or a hard copy by courier, or office pickup in Kyiv."},
		{Title: "Payment", Body: "Bank transfer with or without VAT, card, and cash in the office."},
	}
}

func almost(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestKBOverlap(t *testing.T) {
	kbs := testKB()
	tests := []struct {
		name  string
		query string
		want  float64
	}{
		{"empty", "", 0},
		{"stopwords only", "the of and на в", 0},
		{"all terms present", "certified translation delivery", 1.0}, // certified, translation, delivery
		{"half present", "translation apostille", 0.5},               // translation ✓, apostille ✗
		{"none present", "apostille legalization notary abroad", 0},
		{"stopwords do not count", "is the courier available", 1.0 / 2.0}, // courier ✓, available ✗ (is/the/the dropped)
		{"distinct terms only", "payment payment payment card", 1.0},      // payment ✓ (once), card ✓
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := kbOverlap(tc.query, kbs)
			if !almost(got, tc.want) {
				t.Fatalf("kbOverlap(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestHardEscalate(t *testing.T) {
	hit := []string{
		"Хочу повернути гроші за переклад",
		"Це вже повернення коштів",
		"I want a refund now",
		"Буду писати скаргу",
		"This is a formal complaint",
		"Подам до суду",
		"готую позов",
		"see you in court",
		"дайте людину нарешті",
		"хочу поговорити з людиною",
		"дайте справжню людину",
		"can I talk to a real person",
		"just let me talk to a person",
		"дайте менеджера напряму",
	}
	miss := []string{
		"Скільки коштує переклад диплома?",
		"How much for a certified translation?",
		"Мені потрібен переклад договору",
		"nothing suspicious here",
	}
	for _, s := range hit {
		if !hardEscalate(s) {
			t.Errorf("hardEscalate(%q) = false, want true", s)
		}
	}
	for _, s := range miss {
		if hardEscalate(s) {
			t.Errorf("hardEscalate(%q) = true, want false", s)
		}
	}
}

func TestGroundingGate(t *testing.T) {
	tests := []struct {
		overlap    float64
		slotAnswer bool
		want       bool
	}{
		{0.0, true, false},   // slot answer never escalates
		{0.9, true, false},   // slot answer never escalates
		{0.10, false, true},  // low overlap, not a slot answer -> escalate
		{0.25, false, false}, // exactly the floor is not below it
		{0.30, false, false}, // above floor -> proceed
	}
	for _, tc := range tests {
		if got := groundingGate(tc.overlap, tc.slotAnswer); got != tc.want {
			t.Errorf("groundingGate(%v, %v) = %v, want %v", tc.overlap, tc.slotAnswer, got, tc.want)
		}
	}
}

func TestIsSlotAnswer(t *testing.T) {
	asked := &Session{History: []Msg{{Role: "assistant", Text: "How many pages is it?"}}}
	askedFull := &Session{
		History: []Msg{{Role: "assistant", Text: "Anything else?"}},
		Slots: QuoteSlots{
			LanguagePair: sp("uk->de"), DocType: sp("d"), Volume: sp("2"),
			Deadline: sp("fri"), Certification: sp("none"), Delivery: sp("email"),
		},
	}
	noQuestion := &Session{History: []Msg{{Role: "assistant", Text: "Thanks, noted."}}}
	empty := &Session{}

	tests := []struct {
		name string
		sess *Session
		text string
		want bool
	}{
		{"short answer after a question", asked, "about 5 pages", true},
		{"one word after a question", asked, "diploma", true},
		{"too long after a question", asked, "well it is a diploma with a transcript and also a reference letter", false},
		{"short but no prior question", noQuestion, "5 pages", false},
		{"short, question, but slots complete", askedFull, "no thanks", false},
		{"no history at all", empty, "5 pages", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSlotAnswer(tc.sess, tc.text); got != tc.want {
				t.Fatalf("isSlotAnswer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLangOf(t *testing.T) {
	tests := map[string]string{
		"Доброго дня":      "uk",
		"Hello there":      "en",
		"5 pages, uk->de":  "en", // Latin letters present
		"123 456 !!!":      "",
		"":                 "",
		"diploma переклад": "uk", // any Cyrillic wins
	}
	for in, want := range tests {
		if got := langOf(in); got != want {
			t.Errorf("langOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSessLang(t *testing.T) {
	if got := sessLang(&Session{Lang: "uk"}); got != "uk" {
		t.Errorf("got %q, want uk", got)
	}
	if got := sessLang(&Session{}); got != "en" {
		t.Errorf("empty Lang: got %q, want en (fallback)", got)
	}
}

func sp(s string) *string { return &s }

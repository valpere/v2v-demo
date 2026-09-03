package dialog

import (
	"math"
	"testing"

	"github.com/valpere/v2v-demo/internal/kb"
)

// testKB mirrors the shipped KB's bilingual shape (English block + Ukrainian
// block under one "## " heading) so the gate is exercised in both languages.
func testKB() []kb.Section {
	return []kb.Section{
		{Title: "Services / Послуги", Body: "Written translation, certified translation, notarized translation, sworn translation for German Polish Italian French Czech. Письмовий переклад, засвідчення печаткою бюро, нотаріальний переклад, присяжний переклад."},
		{Title: "Delivery / Доставка", Body: "Scanned PDF by email, or a hard copy by courier, or office pickup in Kyiv. Скан на email, паперовий оригінал кур'єром, самовивіз із офісу."},
		{Title: "Payment / Оплата", Body: "Bank transfer with or without VAT, card, and cryptocurrency. Банківський переказ, картка, криптовалюта."},
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
		{"all terms present", "certified translation delivery", 1.0},
		{"half present", "translation weather", 0.5}, // translation ✓, weather ✗
		{"none present", "weather joke football holiday", 0},
		{"stopwords do not count", "is the courier weather", 1.0 / 2.0}, // courier ✓, weather ✗
		{"distinct terms only", "payment payment payment card", 1.0},
		{"ukrainian exact", "присяжний переклад", 1.0},
		{"ukrainian half present", "нотаріальний футбол", 0.5}, // нотаріальний ✓, футбол ✗
		{"ukrainian inflection stem-matches", "нотаріального засвідчення документів", 2.0 / 3.0},
		// нотаріального ~ нотаріальний ✓, засвідчення exact ✓, документів ✗
		{"stem: криптовалютою → криптовалюта", "оплата криптовалютою", 1.0},
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

func TestStemMatch(t *testing.T) {
	kb := map[string]bool{
		"університети": true, "засвідчення": true, "диплом": true,
		"криптовалюта": true, "київського": true, "переклад": true,
	}
	yes := []string{
		"переклад",      // exact
		"університету",  // -у vs -и, common prefix "університет" (11) ≥ 5
		"засвідчений",   // common prefix "засвідч" (7) ≥ 5
		"диплома",       // "диплом" is a prefix, len 6 ≥ 4
		"криптовалютою", // common prefix "криптовалют" ≥ 5
	}
	no := []string{
		"погода", // no shared stem
		"футбол", // no shared stem
		"києві",  // к-и-є vs к-и-ї (київського) — vowel alternation breaks it at rune 3
	}
	for _, w := range yes {
		if !stemMatch(w, kb) {
			t.Errorf("stemMatch(%q) = false, want true", w)
		}
	}
	for _, w := range no {
		if stemMatch(w, kb) {
			t.Errorf("stemMatch(%q) = true, want false", w)
		}
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
		"I'll take you to court",
		"I will sue you for this",
		"my lawyer is preparing a lawsuit",
		"дайте людину нарешті",
		"хочу поговорити з людиною",
		"дайте справжню людину",
		"can I talk to a real person",
		"just let me talk to a person",
		"дайте менеджера напряму",
		"З'єднайте з менеджером напряму",
		"хочу поговорити з менеджером",
		"can I speak to a manager",
		"Хочу менеджера",
		"дайте менеджера, будь ласка",
		"переключіть на менеджера",
		"I want a manager now",
	}
	miss := []string{
		"Скільки коштує переклад диплома?",
		"How much for a certified translation?",
		"Мені потрібен переклад договору",
		"nothing suspicious here",
		// a court / registry as the document's recipient is a normal
		// certification case, not a legal threat
		"Для суду",
		"Переклад диплома для суду в Німеччині",
		"it's for a court in the UK",
		"to court",
		"send it to court",
		"the document will be used in court",
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
	askedThenClause := &Session{History: []Msg{{Role: "assistant", Text: "Для якої установи ви готуєте документи? Це потрібно, щоб підібрати тип засвідчення."}}}
	empty := &Session{}

	tests := []struct {
		name string
		sess *Session
		text string
		want bool
	}{
		{"short answer after a question", asked, "about 5 pages", true},
		{"one word after a question", asked, "diploma", true},
		{"answer after a question + trailing clause", askedThenClause, "для університету в Берліні", true},
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

func TestIsSmallTalk(t *testing.T) {
	yes := []string{
		"Привіт!", "Добрий день", "Доброго дня ще раз",
		"Дякую. Ви були цілком люб'язні.", "дякую!", "До побачення",
		"Hello", "Hi there", "Thanks a lot", "Bye",
		"Ок", "Зрозуміло", "Добре", "Гаразд", "Ясно", "Ok", "got it", "understood",
	}
	no := []string{
		"Привіт, а скільки коштує апостиль?", // has "?"
		"Яка погода в Києві?",                // question, no marker
		"Мені потрібен переклад диплома",     // content, no marker
		"Доброго дня, розкажіть будь ласка детально про всі ваші тарифи на юридичний переклад великих договорів", // >8 tokens
		"поверніть гроші",
		"Скільки коштує один рядок", // "рядок" must not match " ок"
	}
	for _, s := range yes {
		if !isSmallTalk(s) {
			t.Errorf("isSmallTalk(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if isSmallTalk(s) {
			t.Errorf("isSmallTalk(%q) = true, want false", s)
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

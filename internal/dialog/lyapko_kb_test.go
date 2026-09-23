package dialog

import (
	"testing"

	"github.com/valpere/v2v-demo/internal/kb"
)

// The lyapko KB was first dropped in English-only, so kbOverlap scored
// ordinary Ukrainian openers below GateFloor and the grounding gate clipped
// them pre-LLM (3 of 8 turns in the first dialog-probe run). The fix was a
// Ukrainian block per KB section; this pins it against the shipped file.
func TestLyapkoKBGroundsUkrainianOpeners(t *testing.T) {
	sections, err := kb.Load("../../topics/lyapko/kb.md")
	if err != nil {
		t.Fatalf("load lyapko KB: %v", err)
	}

	grounded := []string{
		"Болить поперек, іноді віддає в ногу",
		"Не сплю вночі, що порадите?",
		"Болить шия після роботи за комп'ютером",
		"Скільки коштує доставка до США?",
	}
	for _, q := range grounded {
		if got := kbOverlap(q, sections); got < GateFloor {
			t.Errorf("kbOverlap(%q) = %.2f, below GateFloor %.2f — the gate would clip a real product question", q, got, GateFloor)
		}
	}

	// control: the Ukrainian block must not turn the gate off for off-topic input
	for _, q := range []string{"Порадь гарний ресторан у Львові", "Розкажи анекдот про котів"} {
		if got := kbOverlap(q, sections); got >= GateFloor {
			t.Errorf("kbOverlap(%q) = %.2f, at/above GateFloor %.2f — off-topic input now slips past the gate", q, got, GateFloor)
		}
	}
}

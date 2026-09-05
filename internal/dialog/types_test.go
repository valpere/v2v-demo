package dialog

import "testing"

func TestComplete(t *testing.T) {
	spec := testSlots() // the translation 6

	full := map[string]string{
		"language_pair": "uk->de", "doc_type": "diploma", "volume": "2 pages",
		"deadline": "Friday", "certification": "notarized", "delivery": "email",
	}
	if !Complete(full, spec) {
		t.Fatal("all six set: want Complete() == true")
	}

	tests := map[string]map[string]string{
		"nil":         nil,
		"empty":       {},
		"one missing": {"language_pair": "uk->de", "doc_type": "d", "volume": "2p", "deadline": "Fri", "certification": "none"},
		"present but blank": {
			"language_pair": "uk->de", "doc_type": "d", "volume": "2p",
			"deadline": "Fri", "certification": "none", "delivery": "",
		},
	}
	for name, sl := range tests {
		if Complete(sl, spec) {
			t.Errorf("%s: want Complete() == false", name)
		}
	}
}

func TestFilledSlots(t *testing.T) {
	spec := testSlots()
	if got := filledSlots(nil, spec); got != 0 {
		t.Fatalf("nil: got %d, want 0", got)
	}
	got := filledSlots(map[string]string{"doc_type": "d", "volume": "2", "unknown": "x"}, spec)
	if got != 2 { // "unknown" is not in the spec — not counted
		t.Fatalf("got %d, want 2", got)
	}
}

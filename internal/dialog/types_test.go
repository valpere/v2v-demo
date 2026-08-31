package dialog

import "testing"

func s(v string) *string { return &v }

func TestQuoteSlotsComplete(t *testing.T) {
	full := QuoteSlots{
		LanguagePair:  s("uk->de"),
		DocType:       s("diploma"),
		Volume:        s("2 pages"),
		Deadline:      s("Friday"),
		Certification: s("notarized"),
		Delivery:      s("email"),
	}
	if !full.Complete() {
		t.Fatal("all six set: want Complete() == true")
	}

	tests := map[string]QuoteSlots{
		"empty":       {},
		"one missing": {LanguagePair: s("uk->de"), DocType: s("diploma"), Volume: s("2p"), Deadline: s("Fri"), Certification: s("none")},
	}
	for name, sl := range tests {
		if sl.Complete() {
			t.Errorf("%s: want Complete() == false", name)
		}
	}
}

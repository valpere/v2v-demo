package kb

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kb.md")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	const doc = `# FromToBridge KB

Fictional. Quotes in EUR.

## Services

- written translation
- certified translation

## Delivery

Email by default.
`
	secs, err := Load(writeTemp(t, doc))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(secs) != 3 {
		t.Fatalf("got %d sections, want 3: %+v", len(secs), secs)
	}

	want := []Section{
		{Title: "FromToBridge KB", Body: "Fictional. Quotes in EUR."},
		{Title: "Services", Body: "- written translation\n- certified translation"},
		{Title: "Delivery", Body: "Email by default."},
	}
	for i, w := range want {
		if secs[i] != w {
			t.Errorf("section %d = %+v, want %+v", i, secs[i], w)
		}
	}
}

func TestLoadNoPreamble(t *testing.T) {
	secs, err := Load(writeTemp(t, "## Only\n\nbody\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(secs) != 1 || secs[0].Title != "Only" || secs[0].Body != "body" {
		t.Fatalf("got %+v", secs)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.md")); err == nil {
		t.Error("missing file: want error")
	}
	if _, err := Load(writeTemp(t, "just prose, no headings\n")); err == nil {
		t.Error("no headings: want error")
	}
}

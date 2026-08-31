// Package kb loads the fictional FromToBridge knowledge base and splits it
// into titled sections on its "## " headings (REQ-KB-01). The whole KB is fed
// to the model on every turn (REQ-DLG-10), so Load preserves every line across
// the sections it returns — nothing is dropped.
package kb

import (
	"fmt"
	"os"
	"strings"
)

// Section is one "## " heading and the text beneath it.
type Section struct {
	Title string // heading text, leading "#" runes and spaces stripped
	Body  string // everything up to the next "## " heading, trimmed
}

// Load reads path and splits it on lines beginning with "## ". Content before
// the first such line (the file's H1 and preamble) becomes the first section,
// titled from its "# " line.
func Load(path string) ([]Section, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("kb: read %s: %w", path, err)
	}

	var (
		sections []Section
		cur      *Section
		body     strings.Builder
		headings int
	)
	flush := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(body.String())
		sections = append(sections, *cur)
		body.Reset()
	}

	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "## "):
			flush()
			headings++
			cur = &Section{Title: strings.TrimSpace(line[len("## "):])}
		case cur == nil && strings.HasPrefix(line, "# "):
			cur = &Section{Title: strings.TrimSpace(line[len("# "):])}
		case cur == nil && strings.TrimSpace(line) == "":
			// blank lines before the first heading are not content
		default:
			if cur == nil {
				cur = &Section{}
			}
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()

	if headings == 0 {
		return nil, fmt.Errorf("kb: %s has no %q sections", path, "## ")
	}
	return sections, nil
}

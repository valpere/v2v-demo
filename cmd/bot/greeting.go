package main

import (
	"fmt"
	"os"
	"strings"
)

// greetingBody extracts the message body from prompt/greeting.md: everything
// after the first line that is exactly "---", with any further "---" lines
// dropped, trimmed. The file's header is never sent (REQ-UX-02).
func greetingBody(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("greeting: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return "", fmt.Errorf("greeting: no '---' separator in %s", path)
	}

	var kept []string
	for _, l := range lines[start:] {
		if strings.TrimSpace(l) == "---" {
			continue
		}
		kept = append(kept, l)
	}
	body := strings.TrimSpace(strings.Join(kept, "\n"))
	if body == "" {
		return "", fmt.Errorf("greeting: empty body in %s", path)
	}
	return body, nil
}

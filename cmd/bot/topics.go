package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/valpere/v2v-demo/internal/kb"
)

// topicManifestEntry is one row of TOPICS_PATH's JSON array.
type topicManifestEntry struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	KB           string `json:"kb"`
	SystemPrompt string `json:"system_prompt"`
	Greeting     string `json:"greeting"`
}

// topicBundle is a fully loaded topic: its own KB, persona and greeting —
// a genuinely different assistant, not just a KB slice of the same one.
type topicBundle struct {
	ID       string
	Title    string
	KB       []kb.Section
	Sys      string
	Greeting string
}

// loadTopics builds the set of topics the bot offers. If cfg.TopicsPath is
// missing, it falls back to one synthetic topic built from
// KB_PATH/SYSTEM_PROMPT_PATH/GREETING_PATH — this is what makes the whole
// feature opt-in: no manifest, no picker, byte-for-byte today's behavior.
// The same fallback applies to a manifest with exactly one entry. ids is
// the stable display order (Go map iteration isn't).
func loadTopics(cfg Config) (topics map[string]topicBundle, ids []string, err error) {
	entries, err := readTopicManifest(cfg.TopicsPath)
	if err != nil {
		return nil, nil, err
	}
	if len(entries) == 0 {
		entries = []topicManifestEntry{{
			ID:           "default",
			Title:        "default",
			KB:           cfg.KBPath,
			SystemPrompt: cfg.SystemPromptPath,
			Greeting:     cfg.GreetingPath,
		}}
	}

	topics = make(map[string]topicBundle, len(entries))
	ids = make([]string, 0, len(entries))
	for _, e := range entries {
		if e.ID == "" {
			return nil, nil, fmt.Errorf("topics: %s: an entry has an empty id", cfg.TopicsPath)
		}
		if _, dup := topics[e.ID]; dup {
			return nil, nil, fmt.Errorf("topics: %s: duplicate id %q", cfg.TopicsPath, e.ID)
		}
		sections, err := kb.Load(e.KB)
		if err != nil {
			return nil, nil, fmt.Errorf("topics: %s (kb): %w", e.ID, err)
		}
		sys, err := os.ReadFile(e.SystemPrompt)
		if err != nil {
			return nil, nil, fmt.Errorf("topics: %s (system prompt): %w", e.ID, err)
		}
		greeting, err := greetingBody(e.Greeting)
		if err != nil {
			return nil, nil, fmt.Errorf("topics: %s (greeting): %w", e.ID, err)
		}
		topics[e.ID] = topicBundle{ID: e.ID, Title: e.Title, KB: sections, Sys: string(sys), Greeting: greeting}
		ids = append(ids, e.ID)
	}
	return topics, ids, nil
}

// readTopicManifest reads TOPICS_PATH. A missing file returns (nil, nil) —
// the caller treats that as "no manifest" and falls back to a single topic.
func readTopicManifest(path string) ([]topicManifestEntry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("topics: open %s: %w", path, err)
	}
	var entries []topicManifestEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("topics: parse %s: %w", path, err)
	}
	return entries, nil
}

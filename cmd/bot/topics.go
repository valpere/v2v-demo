package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
)

// topicManifestEntry is one row of TOPICS_PATH's JSON array.
type topicManifestEntry struct {
	ID           string            `json:"id"`
	Title        string            `json:"title"`
	KB           string            `json:"kb"`
	SystemPrompt string            `json:"system_prompt"`
	Greeting     string            `json:"greeting"`
	ScopeUK      string            `json:"scope_uk"` // "Я допомагаю лише з …" — the clarify line's scope sentence
	ScopeEN      string            `json:"scope_en"`
	Slots        []dialog.SlotSpec `json:"slots"` // what this assistant collects, in ask order
}

// topicBundle is a fully loaded topic: its own KB, persona, greeting and slot
// schema — a genuinely different assistant, not a KB slice of the same one.
type topicBundle struct {
	ID       string
	Title    string
	Greeting string
	Spec     dialog.TopicSpec // KB + system prompt + slots + scope — passed straight to dialog.Handle
}

// defaultTranslationSlots is the 6-slot translation schema, used for the
// synthetic fallback topic when TOPICS_PATH points at nothing.
func defaultTranslationSlots() []dialog.SlotSpec {
	return []dialog.SlotSpec{
		{Key: "language_pair", AskUK: "з якої мови на яку", AskEN: "which languages", Rule: "e.g. uk->de"},
		{Key: "doc_type", AskUK: "який це документ", AskEN: "what kind of document"},
		{Key: "volume", AskUK: "скільки сторінок", AskEN: "how many pages"},
		{Key: "deadline", AskUK: "до якого терміну", AskEN: "the deadline"},
		{Key: "certification", AskUK: "для кого переклад", AskEN: "who it's for", Rule: "none|certified|notarized|sworn|manager to confirm"},
		{Key: "delivery", AskUK: "як доставити", AskEN: "how to deliver it"},
	}
}

const (
	defaultScopeUK = "Я допомагаю лише з перекладами документів."
	defaultScopeEN = "I only help with document translations."
)

// loadTopics builds the set of topics the bot offers. If cfg.TopicsPath is
// missing (or parses to zero entries), it falls back to one synthetic topic
// built from KB_PATH/SYSTEM_PROMPT_PATH/GREETING_PATH + the translation slot
// schema — the opt-in fallback, no picker. A manifest with exactly one entry
// is *not* replaced by the synthetic topic: it loads from that entry's own
// paths and slots, and the caller treats len(topics)==1 the same way — no
// picker. ids is the stable display order (Go map iteration isn't).
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
			ScopeUK:      defaultScopeUK,
			ScopeEN:      defaultScopeEN,
			Slots:        defaultTranslationSlots(),
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
		if err := validateSlots(e.ID, e.Slots); err != nil {
			return nil, nil, err
		}
		if e.ScopeUK == "" || e.ScopeEN == "" {
			return nil, nil, fmt.Errorf("topics: %s: topic %q needs scope_uk and scope_en", cfg.TopicsPath, e.ID)
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
		topics[e.ID] = topicBundle{
			ID:       e.ID,
			Title:    e.Title,
			Greeting: greeting,
			Spec: dialog.TopicSpec{
				KB:      sections,
				System:  string(sys),
				Slots:   e.Slots,
				ScopeUK: e.ScopeUK,
				ScopeEN: e.ScopeEN,
			},
		}
		ids = append(ids, e.ID)
	}
	return topics, ids, nil
}

// validateSlots checks a topic's slot schema: at least one, each with a
// non-empty key and both ask phrasings, and no duplicate keys.
func validateSlots(topicID string, slots []dialog.SlotSpec) error {
	if len(slots) == 0 {
		return fmt.Errorf("topics: topic %q has no slots", topicID)
	}
	seen := make(map[string]bool, len(slots))
	for i, s := range slots {
		if s.Key == "" {
			return fmt.Errorf("topics: topic %q slot #%d has an empty key", topicID, i+1)
		}
		if s.AskUK == "" || s.AskEN == "" {
			return fmt.Errorf("topics: topic %q slot %q needs ask_uk and ask_en", topicID, s.Key)
		}
		if seen[s.Key] {
			return fmt.Errorf("topics: topic %q has a duplicate slot key %q", topicID, s.Key)
		}
		seen[s.Key] = true
	}
	return nil
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

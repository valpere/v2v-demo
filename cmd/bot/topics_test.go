package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeTopicFixture writes a minimal valid KB/system-prompt/greeting trio
// under dir/<id>-{kb,sys,greeting}.md and returns their paths.
func writeTopicFixture(t *testing.T, dir, id string) (kbPath, sysPath, greetPath string) {
	t.Helper()
	kbPath = filepath.Join(dir, id+"-kb.md")
	sysPath = filepath.Join(dir, id+"-sys.md")
	greetPath = filepath.Join(dir, id+"-greeting.md")
	if err := os.WriteFile(kbPath, []byte("## Section\nbody for "+id), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sysPath, []byte("system prompt for "+id), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(greetPath, []byte("header\n---\nGREETING-"+id), 0o644); err != nil {
		t.Fatal(err)
	}
	return kbPath, sysPath, greetPath
}

// validSlots is a manifest slots array that passes validateSlots.
const validSlots = `"scope_uk":"Я допомагаю з X.","scope_en":"I help with X.","slots":[{"key":"a","ask_uk":"а","ask_en":"a-en","rule":""},{"key":"b","ask_uk":"б","ask_en":"b-en","rule":""}]`

// entryJSON is one full, valid manifest entry.
func entryJSON(id, title, kb, sys, greet string) string {
	return fmt.Sprintf(`{"id":%q,"title":%q,"kb":%q,"system_prompt":%q,"greeting":%q,%s}`,
		id, title, kb, sys, greet, validSlots)
}

func baseCfg(t *testing.T, dir string) Config {
	t.Helper()
	kbPath, sysPath, greetPath := writeTopicFixture(t, dir, "default")
	return Config{
		KBPath:           kbPath,
		SystemPromptPath: sysPath,
		GreetingPath:     greetPath,
		TopicsPath:       filepath.Join(dir, "topics.json"), // absent unless a test writes it
	}
}

func TestLoadTopicsMissingManifestFallsBackToSingleSynthetic(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)

	topics, ids, err := loadTopics(cfg)
	if err != nil {
		t.Fatalf("loadTopics: %v", err)
	}
	if len(topics) != 1 || len(ids) != 1 {
		t.Fatalf("got %d topics, want 1: %+v", len(topics), ids)
	}
	got := topics[ids[0]]
	if got.ID != "default" || got.Greeting != "GREETING-default" {
		t.Fatalf("synthetic topic = %+v, want id=default greeting=GREETING-default", got)
	}
}

func TestLoadTopicsEmptyManifestFallsBackToSingleSynthetic(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	if err := os.WriteFile(cfg.TopicsPath, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	topics, ids, err := loadTopics(cfg)
	if err != nil {
		t.Fatalf("loadTopics: %v", err)
	}
	if len(topics) != 1 || ids[0] != "default" {
		t.Fatalf("got %+v / %v, want the single synthetic default topic", topics, ids)
	}
}

func TestLoadTopicsSingleEntryLoadsFromManifestNotConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	kbPath, sysPath, greetPath := writeTopicFixture(t, dir, "notary")
	manifest := "[" + entryJSON("notary", "Нотаріус", kbPath, sysPath, greetPath) + "]"
	if err := os.WriteFile(cfg.TopicsPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	topics, ids, err := loadTopics(cfg)
	if err != nil {
		t.Fatalf("loadTopics: %v", err)
	}
	if len(topics) != 1 || ids[0] != "notary" {
		t.Fatalf("got %+v / %v, want the single manifest entry \"notary\"", topics, ids)
	}
	if got := topics["notary"].Greeting; got != "GREETING-notary" {
		t.Fatalf("greeting = %q, want the manifest entry's own file content, not cfg.GreetingPath", got)
	}
}

func TestLoadTopicsMultipleEntriesPreserveManifestOrder(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	kbA, sysA, greetA := writeTopicFixture(t, dir, "translations")
	kbB, sysB, greetB := writeTopicFixture(t, dir, "notary")
	manifest := "[" + entryJSON("translations", "Переклад", kbA, sysA, greetA) + "," + entryJSON("notary", "Нотаріус", kbB, sysB, greetB) + "]"
	if err := os.WriteFile(cfg.TopicsPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	topics, ids, err := loadTopics(cfg)
	if err != nil {
		t.Fatalf("loadTopics: %v", err)
	}
	if len(topics) != 2 {
		t.Fatalf("got %d topics, want 2", len(topics))
	}
	if len(ids) != 2 || ids[0] != "translations" || ids[1] != "notary" {
		t.Fatalf("ids = %v, want manifest order [translations notary]", ids)
	}
}

func TestLoadTopicsDuplicateIDIsAnError(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	kbPath, sysPath, greetPath := writeTopicFixture(t, dir, "dup")
	manifest := "[" + entryJSON("dup", "A", kbPath, sysPath, greetPath) + "," + entryJSON("dup", "B", kbPath, sysPath, greetPath) + "]"
	if err := os.WriteFile(cfg.TopicsPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := loadTopics(cfg); err == nil {
		t.Fatal("want an error for a duplicate topic id")
	}
}

func TestLoadTopicsEmptyIDIsAnError(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	kbPath, sysPath, greetPath := writeTopicFixture(t, dir, "x")
	manifest := "[" + entryJSON("", "A", kbPath, sysPath, greetPath) + "]"
	if err := os.WriteFile(cfg.TopicsPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := loadTopics(cfg); err == nil {
		t.Fatal("want an error for an empty topic id")
	}
}

func TestLoadTopicsMissingReferencedFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	manifest := "[" + entryJSON("ghost", "A", filepath.Join(dir, "does-not-exist.md"), "x", "y") + "]"
	if err := os.WriteFile(cfg.TopicsPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := loadTopics(cfg); err == nil {
		t.Fatal("want an error when a manifest entry's kb file doesn't exist")
	}
}

func TestLoadTopicsInvalidSlots(t *testing.T) {
	dir := t.TempDir()
	cfg := baseCfg(t, dir)
	kbPath, sysPath, greetPath := writeTopicFixture(t, dir, "x")
	base := fmt.Sprintf(`{"id":"x","title":"X","kb":%q,"system_prompt":%q,"greeting":%q,"scope_uk":"u","scope_en":"e",`,
		kbPath, sysPath, greetPath)

	cases := map[string]string{
		"no slots":       base + `"slots":[]}`,
		"empty key":      base + `"slots":[{"key":"","ask_uk":"а","ask_en":"a"}]}`,
		"missing ask_en": base + `"slots":[{"key":"a","ask_uk":"а"}]}`,
		"duplicate key":  base + `"slots":[{"key":"a","ask_uk":"а","ask_en":"a"},{"key":"a","ask_uk":"б","ask_en":"b"}]}`,
		"missing scope":  fmt.Sprintf(`{"id":"x","title":"X","kb":%q,"system_prompt":%q,"greeting":%q,"slots":[{"key":"a","ask_uk":"а","ask_en":"a"}]}`, kbPath, sysPath, greetPath),
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(cfg.TopicsPath, []byte("["+entry+"]"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := loadTopics(cfg); err == nil {
				t.Fatalf("want an error for %s", name)
			}
		})
	}
}

func TestReadTopicManifestMalformedJSONIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topics.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readTopicManifest(path); err == nil {
		t.Fatal("want an error for malformed JSON")
	}
}

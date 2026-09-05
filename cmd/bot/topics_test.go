package main

import (
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
	manifest := `[{"id":"notary","title":"Нотаріус","kb":"` + kbPath + `","system_prompt":"` + sysPath + `","greeting":"` + greetPath + `"}]`
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
	manifest := `[
		{"id":"translations","title":"Переклад","kb":"` + kbA + `","system_prompt":"` + sysA + `","greeting":"` + greetA + `"},
		{"id":"notary","title":"Нотаріус","kb":"` + kbB + `","system_prompt":"` + sysB + `","greeting":"` + greetB + `"}
	]`
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
	manifest := `[
		{"id":"dup","title":"A","kb":"` + kbPath + `","system_prompt":"` + sysPath + `","greeting":"` + greetPath + `"},
		{"id":"dup","title":"B","kb":"` + kbPath + `","system_prompt":"` + sysPath + `","greeting":"` + greetPath + `"}
	]`
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
	manifest := `[{"id":"","title":"A","kb":"` + kbPath + `","system_prompt":"` + sysPath + `","greeting":"` + greetPath + `"}]`
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
	manifest := `[{"id":"ghost","title":"A","kb":"` + filepath.Join(dir, "does-not-exist.md") + `","system_prompt":"x","greeting":"y"}]`
	if err := os.WriteFile(cfg.TopicsPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := loadTopics(cfg); err == nil {
		t.Fatal("want an error when a manifest entry's kb file doesn't exist")
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

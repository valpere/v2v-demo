// dialog-probe runs docs/smoke-test.md dialogue rows through the REAL
// internal/dialog pipeline (grounding gate + hardEscalate + the LLM) against
// whatever DIALOG_BACKEND / DIALOG_MODEL point at — no Telegram, no bot
// restart. Use it to check the text sections (§2, §6, §7, §8) against a model
// before wiring it in, or to diff two models.
//
// Unlike minions/tgdrive this is part of the main module (it imports
// internal/dialog + internal/kb), so `go build ./...` compiles it.
//
//	go run ./minions/dialog-probe  scenarios.txt
//	go run ./minions/dialog-probe  < scenarios.txt
//	echo 'скільки коштує сторінка' | go run ./minions/dialog-probe
//	go run ./minions/dialog-probe -model gpt-4o-mini -v  scenarios.txt
//
// Scenario file, one turn per line:
//
//	# text            → printed as a header (does NOT reset the session)
//	---   or  blank    → reset: start a fresh Session
//	anything else      → one user turn in the current session
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
)

// topicEntry is the subset of a topics.json row this probe needs.
type topicEntry struct {
	ID           string            `json:"id"`
	KB           string            `json:"kb"`
	SystemPrompt string            `json:"system_prompt"`
	ScopeUK      string            `json:"scope_uk"`
	ScopeEN      string            `json:"scope_en"`
	Slots        []dialog.SlotSpec `json:"slots"`
}

func main() {
	var (
		backend    = flag.String("backend", env("DIALOG_BACKEND", "openai"), "ollama|openai|gemini")
		model      = flag.String("model", env("DIALOG_MODEL", ""), "model id ('' = backend default)")
		topicsPath = flag.String("topics", env("TOPICS_PATH", "topics/topics.json"), "topics.json manifest")
		topicID    = flag.String("topic", "", "topic id to probe ('' = the sole entry)")
		tzName     = flag.String("tz", env("BOT_TIMEZONE", "Europe/Kyiv"), "IANA timezone for the CURRENT TIME block")
		verbose    = flag.Bool("v", false, "print the full reply text, not a one-line preview")
		showSlot   = flag.Bool("slots", false, "print the full slot JSON after each turn")
	)
	flag.Parse()

	topic, err := loadTopic(*topicsPath, *topicID)
	must(err)
	sections, err := kb.Load(topic.KB)
	must(err)
	sysBytes, err := os.ReadFile(topic.SystemPrompt)
	must(err)
	loc, err := time.LoadLocation(*tzName)
	must(err)
	gen, err := newGen(*backend, *model)
	must(err)

	spec := dialog.TopicSpec{
		KB:      sections,
		System:  string(sysBytes),
		Slots:   topic.Slots,
		ScopeUK: topic.ScopeUK,
		ScopeEN: topic.ScopeEN,
	}

	in := os.Stdin
	if flag.NArg() > 0 {
		f, err := os.Open(flag.Arg(0))
		must(err)
		defer f.Close()
		in = f
	}

	sess := &dialog.Session{}
	turn := 0
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	fmt.Printf("backend=%s model=%q  topic=%s  kb=%d sections  tz=%s\n\n", *backend, *model, topic.ID, len(sections), *tzName)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || line == "---":
			if turn > 0 {
				sess = &dialog.Session{}
				turn = 0
				fmt.Println("  ── reset ──")
			}
			continue
		case strings.HasPrefix(line, "#"):
			fmt.Printf("\n%s\n", line)
			continue
		}

		before := compact(sess.Slots)
		start := time.Now()
		reply, _ := dialog.Handle(context.Background(), sess, spec, gen, line, time.Now().In(loc))
		lat := time.Since(start)
		turn++

		tag := fmt.Sprintf("%d KB", len(reply.Matched))
		if lat < 100*time.Millisecond {
			tag = "pre-LLM" // gate / hardEscalate / looksLikeInjection — no model call
		}
		fmt.Printf("  U: %s\n", line)
		fmt.Printf("  [%-9s] %5.1fs  %-8s  %s\n", reply.Signal, lat.Seconds(), tag, delta(before, compact(sess.Slots)))
		if *verbose {
			fmt.Printf("     %s\n", reply.Text)
		} else {
			fmt.Printf("     %s\n", preview(reply.Text, 150))
		}
		if *showSlot {
			fmt.Printf("     slots=%s\n", compact(sess.Slots))
		}
	}
	must(sc.Err())
}

func newGen(backend, model string) (dialog.Generator, error) {
	switch backend {
	case "openai":
		return dialog.NewOpenAI(env("OPENAI_API_KEY", ""), model), nil
	case "gemini":
		return dialog.NewGemini(env("GEMINI_API_KEY", ""), model), nil
	case "ollama":
		return dialog.NewOllama(env("OLLAMA_BASE_URL", "http://localhost:11434"), model), nil
	default:
		return nil, fmt.Errorf("unknown backend %q", backend)
	}
}

// env reads a var from the process environment, falling back to a KEY=VALUE
// line in ./.env (tiny parser — no quotes, no export, "#" comments).
func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	f, err := os.Open(".env")
	if err != nil {
		return fallback
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(k) != key {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		if v != "" {
			return v
		}
	}
	return fallback
}

// loadTopic reads topics.json and returns the requested entry (or the sole
// one when id is "").
func loadTopic(path, id string) (topicEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return topicEntry{}, err
	}
	var entries []topicEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return topicEntry{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(entries) == 0 {
		return topicEntry{}, fmt.Errorf("%s has no topics", path)
	}
	if id == "" {
		if len(entries) != 1 {
			ids := make([]string, len(entries))
			for i, e := range entries {
				ids[i] = e.ID
			}
			return topicEntry{}, fmt.Errorf("%s has %d topics (%s) — pass -topic", path, len(entries), strings.Join(ids, ", "))
		}
		return entries[0], nil
	}
	for _, e := range entries {
		if e.ID == id {
			return e, nil
		}
	}
	return topicEntry{}, fmt.Errorf("%s has no topic %q", path, id)
}

func compact(s map[string]string) map[string]string {
	m := make(map[string]string, len(s))
	for k, v := range s {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

func delta(before, after map[string]string) string {
	var out []string
	for k, v := range after {
		if before[k] != v {
			out = append(out, k+"="+v)
		}
	}
	if len(out) == 0 {
		return "(no slot change)"
	}
	return strings.Join(out, " ")
}

func preview(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "dialog-probe:", err)
		os.Exit(1)
	}
}

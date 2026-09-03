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
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
)

func main() {
	var (
		backend  = flag.String("backend", env("DIALOG_BACKEND", "openai"), "ollama|openai|gemini")
		model    = flag.String("model", env("DIALOG_MODEL", ""), "model id ('' = backend default)")
		kbPath   = flag.String("kb", env("KB_PATH", "kb/translation-bureau.md"), "KB path")
		sysPath  = flag.String("prompt", env("SYSTEM_PROMPT_PATH", "prompt/system.md"), "system prompt path")
		tzName   = flag.String("tz", env("BOT_TIMEZONE", "Europe/Kyiv"), "IANA timezone for the CURRENT TIME block")
		verbose  = flag.Bool("v", false, "print the full reply text, not a one-line preview")
		showSlot = flag.Bool("slots", false, "print the full slot JSON after each turn")
	)
	flag.Parse()

	sections, err := kb.Load(*kbPath)
	must(err)
	sysBytes, err := os.ReadFile(*sysPath)
	must(err)
	loc, err := time.LoadLocation(*tzName)
	must(err)
	gen, err := newGen(*backend, *model)
	must(err)

	in := os.Stdin
	if flag.NArg() > 0 {
		f, err := os.Open(flag.Arg(0))
		must(err)
		defer f.Close()
		in = f
	}

	sys := string(sysBytes)
	sess := &dialog.Session{}
	turn := 0
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	fmt.Printf("backend=%s model=%q  kb=%d sections  tz=%s\n\n", *backend, *model, len(sections), *tzName)

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
		reply, _ := dialog.Handle(context.Background(), sess, sections, gen, sys, line, time.Now().In(loc))
		lat := time.Since(start)
		turn++

		tag := "pre-LLM"
		if reply.Matched != nil {
			tag = fmt.Sprintf("%d KB", len(reply.Matched))
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

func compact(s dialog.QuoteSlots) map[string]string {
	m := map[string]string{}
	for k, p := range map[string]*string{
		"language_pair": s.LanguagePair, "doc_type": s.DocType, "volume": s.Volume,
		"deadline": s.Deadline, "certification": s.Certification, "delivery": s.Delivery,
	} {
		if p != nil {
			m[k] = *p
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

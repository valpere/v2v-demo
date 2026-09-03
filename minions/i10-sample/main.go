// i10-sample builds the I-10 demo audio: the three .engage/client-demo.md §3
// dialogues, the bot's replies produced by the real dialog.Handle pipeline
// (DIALOG_BACKEND/DIALOG_MODEL from .env), every line voiced with ElevenLabs
// — Vira on voice A, the client on voice B — stitched into one mp3.
//
// It is a *synthesised* sample (both sides are TTS), not a recording of a
// live session. The client makes their own live recording in the bot.
//
//	go run ./minions/i10-sample            # -> tmp/i10-sample.mp3
//	go run ./minions/i10-sample -out x.mp3
//
// Needs: OPENAI_API_KEY + ELEVENLABS_API_KEY + ELEVENLABS_VOICE_A/B in .env,
// and ffmpeg on PATH.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/valpere/v2v-demo/internal/dialog"
	"github.com/valpere/v2v-demo/internal/kb"
	"github.com/valpere/v2v-demo/internal/tts"
)

type line struct {
	who  string // "client" | "vira"
	lang string // "uk" | "en"
	text string // for a client line: verbatim; for vira: filled from Handle
}

// the three dialogues — client lines only; Vira's are produced by Handle.
var dialogues = [][]line{
	{
		{"client", "uk", "Добрий день! Мені треба перекласти диплом з української на німецьку, для вступу в університет у Берліні."},
		{"client", "uk", "Диплом на одну сторінку і додаток на дві, разом три. Бажано за тиждень."},
		{"client", "uk", "Вони писали \"certified translation\". Достатньо печатки бюро. Скан на пошту."},
	},
	{
		{"client", "en", "Hi, how much do you charge for translation?"},
	},
	{
		{"client", "uk", "Доброго дня. Мені треба присяжний переклад свідоцтва про народження на іспанську?"},
	},
}

func main() {
	out := flag.String("out", "tmp/i10-sample.mp3", "output mp3")
	pause := flag.Float64("pause", 0.7, "seconds of silence between lines")
	gap := flag.Float64("gap", 1.8, "seconds of silence between dialogues")
	flag.Parse()

	key := env("OPENAI_API_KEY")
	if key == "" {
		die("OPENAI_API_KEY not set (env or .env)")
	}
	elevenKey := env("ELEVENLABS_API_KEY")
	voiceA, voiceB := env("ELEVENLABS_VOICE_A"), env("ELEVENLABS_VOICE_B")
	if elevenKey == "" || voiceA == "" || voiceB == "" {
		die("ELEVENLABS_API_KEY / ELEVENLABS_VOICE_A / ELEVENLABS_VOICE_B not set")
	}

	sections, err := kb.Load(env2("KB_PATH", "kb/translation-bureau.md"))
	must(err)
	sysBytes, err := os.ReadFile(env2("SYSTEM_PROMPT_PATH", "prompt/system.md"))
	must(err)
	gen := dialog.NewOpenAI(key, env("DIALOG_MODEL"))
	synth := tts.NewElevenLabs(elevenKey)
	loc, _ := time.LoadLocation(env2("BOT_TIMEZONE", "Europe/Kyiv"))
	if loc == nil {
		loc = time.UTC
	}

	work, err := os.MkdirTemp("", "i10-*")
	must(err)
	defer os.RemoveAll(work)

	var clips []string
	sil := filepath.Join(work, "sil.ogg")
	gapClip := filepath.Join(work, "gap.ogg")
	mkSilence(sil, *pause)
	mkSilence(gapClip, *gap)

	titles := []string{
		"Діалог 1 — повне котирування, українською",
		"Діалог 2 — питання про ціну, англійською",
		"Діалог 3 — передача менеджеру, українською",
	}
	var script strings.Builder
	fmt.Fprintf(&script, "I-10 sample — %s\nbot replies: dialog.Handle (openai/%s)\nVira = voice A, client = voice B\n",
		time.Now().Format("2006-01-02 15:04"), or(env("DIALOG_MODEL"), "gpt-4.1-mini"))

	ctx := context.Background()
	for di, d := range dialogues {
		if di > 0 {
			clips = append(clips, gapClip)
		}
		fmt.Fprintf(&script, "\n\n== %s ==\n", titles[di])
		fmt.Printf("── %s ──\n", titles[di])
		sess := &dialog.Session{}
		for li, ln := range d {
			// client line
			clips = append(clips, speak(ctx, synth, work, fmt.Sprintf("d%d-%02dc", di, li), ln.text, voiceB, ln.lang))
			fmt.Fprintf(&script, "\nКлієнт: %s\n", ln.text)
			fmt.Printf("  C: %s\n", ln.text)
			clips = append(clips, sil)
			// Vira's reply
			reply, _ := dialog.Handle(ctx, sess, sections, gen, string(sysBytes), ln.text, time.Now().In(loc))
			spoken := tts.Spoken(reply.Text, ln.lang)
			fmt.Fprintf(&script, "Віра [%s]: %s\n", reply.Signal, strings.TrimSpace(reply.Text))
			fmt.Printf("  V[%s]: %s\n", reply.Signal, oneline(reply.Text))
			clips = append(clips, speak(ctx, synth, work, fmt.Sprintf("d%d-%02dv", di, li), spoken, voiceA, ln.lang))
			if li < len(d)-1 {
				clips = append(clips, sil)
			}
		}
	}

	listFile := filepath.Join(work, "list.txt")
	var b strings.Builder
	for _, c := range clips {
		fmt.Fprintf(&b, "file '%s'\n", c)
	}
	must(os.WriteFile(listFile, []byte(b.String()), 0o644))

	must(os.MkdirAll(filepath.Dir(*out), 0o755))
	run("ffmpeg", "-y", "-f", "concat", "-safe", "0", "-i", listFile,
		"-af", "dynaudnorm", "-c:a", "libmp3lame", "-q:a", "2", *out)

	txt := strings.TrimSuffix(*out, filepath.Ext(*out)) + ".txt"
	must(os.WriteFile(txt, []byte(script.String()), 0o644))
	fmt.Printf("\n✓ %s\n✓ %s\n", *out, txt)
}

func speak(ctx context.Context, s tts.Synthesizer, dir, name, text, voice, lang string) string {
	ogg, err := s.Speak(ctx, text, voice, lang)
	must(err)
	p := filepath.Join(dir, name+".ogg")
	must(os.WriteFile(p, ogg, 0o644))
	return p
}

func mkSilence(path string, secs float64) {
	run("ffmpeg", "-y", "-f", "lavfi", "-i", "anullsrc=r=48000:cl=mono",
		"-t", fmt.Sprintf("%.2f", secs), "-c:a", "libopus", "-b:a", "128k", path)
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = nil
	if out, err := cmd.CombinedOutput(); err != nil {
		die(fmt.Sprintf("%s: %v\n%s", name, err, out))
	}
}

func oneline(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if r := []rune(s); len(r) > 90 {
		return string(r[:90]) + "…"
	}
	return s
}

func env(key string) string { return env2(key, "") }

func env2(key, fallback string) string {
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
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
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

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
func die(msg string) {
	fmt.Fprintln(os.Stderr, "i10-sample:", msg)
	os.Exit(1)
}

func or(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

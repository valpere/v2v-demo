package main

import (
	"os"
	"path/filepath"
	"testing"
)

// chdir into a temp dir holding the given .env, restoring afterwards.
func chdirWithEnv(t *testing.T, dotenv string) {
	t.Helper()
	dir := t.TempDir()
	if dotenv != "" {
		if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(dotenv), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

func TestReadDotEnv(t *testing.T) {
	chdirWithEnv(t, "# a comment\n\nFOO=bar\nBAZ = qux \nWITH_INLINE=elevenlabs   # not azure\nexport EXPORTED=yes\n")
	vals, err := readDotEnv(".env")
	if err != nil {
		t.Fatalf("readDotEnv: %v", err)
	}
	want := map[string]string{"FOO": "bar", "BAZ": "qux", "WITH_INLINE": "elevenlabs", "EXPORTED": "yes"}
	for k, w := range want {
		if vals[k] != w {
			t.Errorf("%s = %q, want %q", k, vals[k], w)
		}
	}
}

func TestReadDotEnvMissing(t *testing.T) {
	chdirWithEnv(t, "")
	vals, err := readDotEnv(".env")
	if err != nil || len(vals) != 0 {
		t.Fatalf("missing .env: got %v, %v", vals, err)
	}
}

func TestReadDotEnvMalformed(t *testing.T) {
	chdirWithEnv(t, "NOT_A_PAIR\n")
	if _, err := readDotEnv(".env"); err == nil {
		t.Fatal("malformed line: want error")
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	chdirWithEnv(t, "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_API_KEY=k\n")
	// ensure the process env doesn't shadow the file
	for _, k := range []string{"TELEGRAM_BOT_TOKEN", "ELEVENLABS_API_KEY", "TTS_BACKEND", "STT_BACKEND", "DIALOG_BACKEND", "DIALOG_MODEL", "DATA_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TTSBackend != "elevenlabs" || cfg.STTBackend != "local" ||
		cfg.DialogBackend != "ollama" || cfg.DialogModel != "gemma4:cloud" ||
		cfg.OllamaBaseURL != "http://localhost:11434" || cfg.DataDir != "./data" ||
		cfg.KBPath != "kb/translation-bureau.md" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	chdirWithEnv(t, "TELEGRAM_BOT_TOKEN=fromfile\nELEVENLABS_API_KEY=k\n")
	t.Setenv("TELEGRAM_BOT_TOKEN", "fromenv")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TelegramToken != "fromenv" {
		t.Fatalf("env should win: got %q", cfg.TelegramToken)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	cases := map[string]string{
		"no telegram token":  "ELEVENLABS_API_KEY=k\n",
		"no eleven key":      "TELEGRAM_BOT_TOKEN=t\n",
		"bad tts backend":    "TELEGRAM_BOT_TOKEN=t\nTTS_BACKEND=festival\n",
		"gemini without key": "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_API_KEY=k\nDIALOG_BACKEND=gemini\n",
		"openai stt no key":  "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_API_KEY=k\nSTT_BACKEND=openai\n",
	}
	for name, dotenv := range cases {
		t.Run(name, func(t *testing.T) {
			chdirWithEnv(t, dotenv)
			for _, k := range []string{"TELEGRAM_BOT_TOKEN", "ELEVENLABS_API_KEY", "TTS_BACKEND", "STT_BACKEND", "DIALOG_BACKEND", "GEMINI_API_KEY", "OPENAI_API_KEY"} {
				os.Unsetenv(k)
			}
			if _, err := LoadConfig(); err == nil {
				t.Fatal("want validation error")
			}
		})
	}
}

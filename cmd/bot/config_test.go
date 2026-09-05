package main

import (
	"os"
	"path/filepath"
	"testing"
)

// every env var LoadConfig looks at — blanked so a value in the dev
// environment (e.g. an exported GEMINI_API_KEY) never leaks into a test.
var configEnvKeys = []string{
	"TELEGRAM_BOT_TOKEN",
	"TTS_BACKEND", "ELEVENLABS_API_KEY", "ELEVENLABS_VOICE_A", "ELEVENLABS_VOICE_B",
	"AZURE_SPEECH_KEY", "AZURE_SPEECH_REGION", "AZURE_VOICE_A", "AZURE_VOICE_B",
	"STT_BACKEND", "WHISPER_BIN", "WHISPER_MODEL", "WHISPER_LANG",
	"DIALOG_BACKEND", "DIALOG_MODEL", "GEMINI_API_KEY", "OLLAMA_BASE_URL", "OPENAI_API_KEY",
	"KB_PATH", "SYSTEM_PROMPT_PATH", "GREETING_PATH", "DATA_DIR",
	"BOT_TIMEZONE",
	"SESSION_STORE", "SESSION_DB_PATH",
	"TOPICS_PATH",
}

// chdirWithEnv chdirs into a temp dir holding the given .env and blanks every
// config env var (t.Setenv restores them afterwards).
func chdirWithEnv(t *testing.T, dotenv string) {
	t.Helper()
	for _, k := range configEnvKeys {
		t.Setenv(k, "")
	}
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

// minValidEnv is the smallest .env that passes validate() on the dev defaults.
const minValidEnv = "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_API_KEY=k\nELEVENLABS_VOICE_A=va\nELEVENLABS_VOICE_B=vb\n"

func TestLoadConfigDefaults(t *testing.T) {
	chdirWithEnv(t, minValidEnv)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	checks := map[string][2]string{
		"TTSBackend":    {cfg.TTSBackend, "elevenlabs"},
		"STTBackend":    {cfg.STTBackend, "local"},
		"WhisperModel":  {cfg.WhisperModel, "turbo"},
		"WhisperLang":   {cfg.WhisperLang, "uk"},
		"DialogBackend": {cfg.DialogBackend, "ollama"},
		"DialogModel":   {cfg.DialogModel, ""}, // "" → the generator picks its backend default
		"OllamaBaseURL": {cfg.OllamaBaseURL, "http://localhost:11434"},
		"DataDir":       {cfg.DataDir, "./data"},
		"KBPath":        {cfg.KBPath, "kb/translation-bureau.md"},
		"Timezone":      {cfg.Timezone, "Europe/Kyiv"},
		"SessionStore":  {cfg.SessionStore, "memory"},
		"SessionDBPath": {cfg.SessionDBPath, "./data/sessions.db"},
		"TopicsPath":    {cfg.TopicsPath, "topics/topics.json"},
	}
	for field, cw := range checks {
		if cw[0] != cw[1] {
			t.Errorf("%s = %q, want %q", field, cw[0], cw[1])
		}
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	chdirWithEnv(t, minValidEnv+"TELEGRAM_BOT_TOKEN=fromfile\n")
	t.Setenv("TELEGRAM_BOT_TOKEN", "fromenv")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.TelegramToken != "fromenv" {
		t.Fatalf("env should win over file")
	}
}

func TestLoadConfigValidation(t *testing.T) {
	cases := map[string]string{
		"no telegram token":   "ELEVENLABS_API_KEY=k\nELEVENLABS_VOICE_A=a\nELEVENLABS_VOICE_B=b\n",
		"no eleven key":       "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_VOICE_A=a\nELEVENLABS_VOICE_B=b\n",
		"no eleven voice ids": "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_API_KEY=k\n",
		"one eleven voice id": "TELEGRAM_BOT_TOKEN=t\nELEVENLABS_API_KEY=k\nELEVENLABS_VOICE_A=a\n",
		"bad tts backend":     "TELEGRAM_BOT_TOKEN=t\nTTS_BACKEND=festival\n",
		"gemini without key":  minValidEnv + "DIALOG_BACKEND=gemini\n",
		"openai stt no key":   minValidEnv + "STT_BACKEND=openai\n",
		"azure no key":        "TELEGRAM_BOT_TOKEN=t\nTTS_BACKEND=azure\n",
		"bad timezone":        minValidEnv + "BOT_TIMEZONE=Mars/Olympus\n",
		"bad stt backend":     "TELEGRAM_BOT_TOKEN=t\nSTT_BACKEND=carrier-pigeon\n",
		"bad session store":   "TELEGRAM_BOT_TOKEN=t\nSESSION_STORE=redis\n",
	}
	for name, dotenv := range cases {
		t.Run(name, func(t *testing.T) {
			chdirWithEnv(t, dotenv)
			if _, err := LoadConfig(); err == nil {
				t.Fatal("want validation error")
			}
		})
	}
}

func TestLoadConfigTimezoneOverride(t *testing.T) {
	chdirWithEnv(t, minValidEnv+"BOT_TIMEZONE=UTC\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("BOT_TIMEZONE=UTC should validate: %v", err)
	}
	if cfg.Timezone != "UTC" {
		t.Fatalf("Timezone = %q, want UTC", cfg.Timezone)
	}
}

func TestLoadConfigSessionStoreSQLite(t *testing.T) {
	chdirWithEnv(t, minValidEnv+"SESSION_STORE=sqlite\nSESSION_DB_PATH=/tmp/x.db\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("SESSION_STORE=sqlite should validate: %v", err)
	}
	if cfg.SessionStore != "sqlite" || cfg.SessionDBPath != "/tmp/x.db" {
		t.Fatalf("got %+v", cfg)
	}
}

func TestLoadConfigTTSNone(t *testing.T) {
	chdirWithEnv(t, "TELEGRAM_BOT_TOKEN=t\nTTS_BACKEND=none\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("TTS_BACKEND=none should validate with no voice keys: %v", err)
	}
	if cfg.TTSBackend != "none" {
		t.Fatalf("TTSBackend = %q", cfg.TTSBackend)
	}
}

func TestLoadConfigSTTNone(t *testing.T) {
	chdirWithEnv(t, "TELEGRAM_BOT_TOKEN=t\nTTS_BACKEND=none\nSTT_BACKEND=none\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("STT_BACKEND=none should validate with no key: %v", err)
	}
	if cfg.STTBackend != "none" {
		t.Fatalf("STTBackend = %q", cfg.STTBackend)
	}
}

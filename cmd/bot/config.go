package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is the whole runtime configuration (REQ-NFR-05: env only, or a local
// .env). It mirrors @schema Config in docs/requirements.md §1.
type Config struct {
	TelegramToken string

	TTSBackend   string // "elevenlabs" | "azure"
	ElevenKey    string
	ElevenVoiceA string
	ElevenVoiceB string
	AzureKey     string
	AzureRegion  string
	AzureVoiceA  string
	AzureVoiceB  string

	STTBackend   string // "local" (default) | "openai"
	WhisperBin   string
	WhisperModel string
	WhisperLang  string // "auto" | "uk" | "en"

	DialogBackend string // "ollama" (default) | "openai" | "gemini"
	DialogModel   string
	GeminiKey     string
	OllamaBaseURL string
	OpenAIKey     string

	KBPath           string
	SystemPromptPath string
	GreetingPath     string
	DataDir          string

	Timezone string // IANA name for the bureau's local time (BOT_TIMEZONE); the
	// server may be UTC, so this is what the "office hours" prompt block uses
}

// LoadConfig builds Config from the process environment, falling back to a
// local .env file for any key not set (or set empty) in the environment.
func LoadConfig() (Config, error) {
	fileVals, err := readDotEnv(".env")
	if err != nil {
		return Config{}, err
	}
	get := func(key string) string {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			return v
		}
		return fileVals[key]
	}
	def := func(key, fallback string) string {
		if v := get(key); v != "" {
			return v
		}
		return fallback
	}

	cfg := Config{
		TelegramToken: get("TELEGRAM_BOT_TOKEN"),

		TTSBackend:   def("TTS_BACKEND", "elevenlabs"),
		ElevenKey:    get("ELEVENLABS_API_KEY"),
		ElevenVoiceA: get("ELEVENLABS_VOICE_A"),
		ElevenVoiceB: get("ELEVENLABS_VOICE_B"),
		AzureKey:     get("AZURE_SPEECH_KEY"),
		AzureRegion:  get("AZURE_SPEECH_REGION"),
		AzureVoiceA:  def("AZURE_VOICE_A", "uk-UA-PolinaNeural"),
		AzureVoiceB:  def("AZURE_VOICE_B", "uk-UA-OstapNeural"),

		STTBackend:   def("STT_BACKEND", "local"),
		WhisperBin:   def("WHISPER_BIN", "whisper"),
		WhisperModel: def("WHISPER_MODEL", "turbo"),
		WhisperLang:  def("WHISPER_LANG", "uk"),

		DialogBackend: def("DIALOG_BACKEND", "ollama"),
		DialogModel:   get("DIALOG_MODEL"), // "" → the generator picks its backend default

		GeminiKey:     get("GEMINI_API_KEY"),
		OllamaBaseURL: def("OLLAMA_BASE_URL", "http://localhost:11434"),
		OpenAIKey:     get("OPENAI_API_KEY"),

		KBPath:           def("KB_PATH", "kb/translation-bureau.md"),
		SystemPromptPath: def("SYSTEM_PROMPT_PATH", "prompt/system.md"),
		GreetingPath:     def("GREETING_PATH", "prompt/greeting.md"),
		DataDir:          def("DATA_DIR", "./data"),

		Timezone: def("BOT_TIMEZONE", "Europe/Kyiv"),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate checks the required keys for the selected backends only — the
// alternates (Azure, OpenAI, Gemini) are not needed for the dev path.
func (c Config) validate() error {
	var errs []string
	if c.TelegramToken == "" {
		errs = append(errs, "TELEGRAM_BOT_TOKEN is required")
	}

	switch c.TTSBackend {
	case "none":
		// voice replies disabled — text only (dev / bulk smoke-testing)
	case "elevenlabs":
		if c.ElevenKey == "" {
			errs = append(errs, "ELEVENLABS_API_KEY is required for TTS_BACKEND=elevenlabs")
		}
		if c.ElevenVoiceA == "" || c.ElevenVoiceB == "" {
			errs = append(errs, "ELEVENLABS_VOICE_A and ELEVENLABS_VOICE_B are required for TTS_BACKEND=elevenlabs")
		}
	case "azure":
		if c.AzureKey == "" || c.AzureRegion == "" {
			errs = append(errs, "AZURE_SPEECH_KEY and AZURE_SPEECH_REGION are required for TTS_BACKEND=azure")
		}
		if c.AzureVoiceA == "" || c.AzureVoiceB == "" {
			errs = append(errs, "AZURE_VOICE_A and AZURE_VOICE_B are required for TTS_BACKEND=azure")
		}
	default:
		errs = append(errs, fmt.Sprintf("TTS_BACKEND %q: want none|elevenlabs|azure", c.TTSBackend))
	}

	switch c.STTBackend {
	case "none":
		// voice input disabled — the bot asks the client to type instead
	case "local":
		// no key needed
	case "openai":
		if c.OpenAIKey == "" {
			errs = append(errs, "OPENAI_API_KEY is required for STT_BACKEND=openai")
		}
	default:
		errs = append(errs, fmt.Sprintf("STT_BACKEND %q: want none|local|openai", c.STTBackend))
	}

	switch c.DialogBackend {
	case "ollama":
		// uses OLLAMA_BASE_URL, which has a default
	case "openai":
		if c.OpenAIKey == "" {
			errs = append(errs, "OPENAI_API_KEY is required for DIALOG_BACKEND=openai")
		}
	case "gemini":
		if c.GeminiKey == "" {
			errs = append(errs, "GEMINI_API_KEY is required for DIALOG_BACKEND=gemini")
		}
	default:
		errs = append(errs, fmt.Sprintf("DIALOG_BACKEND %q: want ollama|openai|gemini", c.DialogBackend))
	}

	if _, err := time.LoadLocation(c.Timezone); err != nil {
		errs = append(errs, fmt.Sprintf("BOT_TIMEZONE %q: %v", c.Timezone, err))
	}

	if len(errs) > 0 {
		return errors.New("config: " + strings.Join(errs, "; "))
	}
	return nil
}

// readDotEnv parses a tiny .env: KEY=VALUE lines, "#" comments (whole-line and
// trailing " #..."), no quotes, no multiline, no "export ". A missing file is
// not an error.
func readDotEnv(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("config: open %s: %w", path, err)
	}
	defer f.Close()

	vals := map[string]string{}
	sc := bufio.NewScanner(f)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("config: %s:%d: not KEY=VALUE", path, lineNo)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		vals[key] = val
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return vals, nil
}

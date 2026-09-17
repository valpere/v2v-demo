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

	TTSBackend         string // "elevenlabs" | "azure" | "espeak"
	TTSFallbackBackend string // "" (default, no failover) | "elevenlabs" | "azure" | "espeak" — tried when TTSBackend errors
	ElevenKey          string
	ElevenVoiceA       string
	ElevenVoiceB       string
	AzureKey           string
	AzureRegion        string
	AzureVoiceA        string
	AzureVoiceB        string
	EspeakBin          string // default "espeak-ng"

	STTBackend         string // "local" (default) | "openai"
	STTFallbackBackend string // "" (default, no failover) | "local" | "openai" — tried when STTBackend errors
	WhisperBin         string
	WhisperModel       string
	WhisperLang        string // "auto" | "uk" | "en"

	DialogBackend         string // "ollama" (default) | "openai" | "gemini"
	DialogFallbackBackend string // "" (default, no failover) | "ollama" | "openai" | "gemini" — tried when DialogBackend errors
	DialogModel           string
	GeminiKey             string
	OllamaBaseURL         string
	OpenAIKey             string

	KBPath           string
	SystemPromptPath string
	GreetingPath     string
	DataDir          string

	Timezone string // IANA name for the bureau's local time (BOT_TIMEZONE); the
	// server may be UTC, so this is what the "office hours" prompt block uses

	SessionStore  string // "memory" (default) | "sqlite" (SESSION_STORE)
	SessionDBPath string // SQLite file, only used when SessionStore=="sqlite"

	TopicsPath string // topics.json manifest (TOPICS_PATH); missing file -> one synthetic topic from KBPath/SystemPromptPath/GreetingPath, no picker shown
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

		TTSBackend:         def("TTS_BACKEND", "elevenlabs"),
		TTSFallbackBackend: get("TTS_FALLBACK_BACKEND"), // "" → no failover
		ElevenKey:          get("ELEVENLABS_API_KEY"),
		ElevenVoiceA:       get("ELEVENLABS_VOICE_A"),
		ElevenVoiceB:       get("ELEVENLABS_VOICE_B"),
		AzureKey:           get("AZURE_SPEECH_KEY"),
		AzureRegion:        get("AZURE_SPEECH_REGION"),
		AzureVoiceA:        def("AZURE_VOICE_A", "uk-UA-PolinaNeural"),
		AzureVoiceB:        def("AZURE_VOICE_B", "uk-UA-OstapNeural"),
		EspeakBin:          def("ESPEAK_BIN", "espeak-ng"),

		STTBackend:         def("STT_BACKEND", "local"),
		STTFallbackBackend: get("STT_FALLBACK_BACKEND"), // "" → no failover
		WhisperBin:         def("WHISPER_BIN", "whisper"),
		WhisperModel:       def("WHISPER_MODEL", "turbo"),
		WhisperLang:        def("WHISPER_LANG", "uk"),

		DialogBackend:         def("DIALOG_BACKEND", "ollama"),
		DialogFallbackBackend: get("DIALOG_FALLBACK_BACKEND"), // "" → no failover
		DialogModel:           get("DIALOG_MODEL"),            // "" → the generator picks its backend default

		GeminiKey:     get("GEMINI_API_KEY"),
		OllamaBaseURL: def("OLLAMA_BASE_URL", "http://localhost:11434"),
		OpenAIKey:     get("OPENAI_API_KEY"),

		KBPath:           def("KB_PATH", "topics/translation/kb.md"),
		SystemPromptPath: def("SYSTEM_PROMPT_PATH", "topics/translation/system.md"),
		GreetingPath:     def("GREETING_PATH", "topics/translation/greeting.md"),
		DataDir:          def("DATA_DIR", "./data"),

		Timezone: def("BOT_TIMEZONE", "Europe/Kyiv"),

		SessionStore:  def("SESSION_STORE", "memory"),
		SessionDBPath: def("SESSION_DB_PATH", "./data/sessions.db"),

		TopicsPath: def("TOPICS_PATH", "topics/topics.json"),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// requireTTSKey checks the key(s) a given TTS backend name needs. envVar
// names the field in the error message (TTS_BACKEND or
// TTS_FALLBACK_BACKEND) so a bad fallback name is diagnosable at startup
// same as a bad primary one.
func (c Config) requireTTSKey(name, envVar string) []string {
	switch name {
	case "none":
		return nil // voice replies disabled — text only (dev / bulk smoke-testing)
	case "elevenlabs":
		var errs []string
		if c.ElevenKey == "" {
			errs = append(errs, "ELEVENLABS_API_KEY is required for "+envVar+"=elevenlabs")
		}
		if c.ElevenVoiceA == "" || c.ElevenVoiceB == "" {
			errs = append(errs, "ELEVENLABS_VOICE_A and ELEVENLABS_VOICE_B are required for "+envVar+"=elevenlabs")
		}
		return errs
	case "azure":
		var errs []string
		if c.AzureKey == "" || c.AzureRegion == "" {
			errs = append(errs, "AZURE_SPEECH_KEY and AZURE_SPEECH_REGION are required for "+envVar+"=azure")
		}
		if c.AzureVoiceA == "" || c.AzureVoiceB == "" {
			errs = append(errs, "AZURE_VOICE_A and AZURE_VOICE_B are required for "+envVar+"=azure")
		}
		return errs
	case "espeak":
		return nil // no key needed
	default:
		return []string{fmt.Sprintf("%s %q: want none|elevenlabs|azure|espeak", envVar, name)}
	}
}

// requireSTTKey checks the key(s) a given STT backend name needs. envVar
// names the field in the error message (STT_BACKEND or
// STT_FALLBACK_BACKEND) so a bad fallback name is diagnosable at startup
// same as a bad primary one.
func (c Config) requireSTTKey(name, envVar string) []string {
	switch name {
	case "none":
		return nil // voice input disabled — the bot asks the client to type instead
	case "local":
		return nil // no key needed
	case "openai":
		if c.OpenAIKey == "" {
			return []string{"OPENAI_API_KEY is required for " + envVar + "=openai"}
		}
		return nil
	default:
		return []string{fmt.Sprintf("%s %q: want none|local|openai", envVar, name)}
	}
}

// requireDialogKey checks the key(s) a given dialogue backend name needs.
// See requireSTTKey for the envVar parameter's purpose.
func (c Config) requireDialogKey(name, envVar string) []string {
	switch name {
	case "ollama":
		return nil // uses OLLAMA_BASE_URL, which has a default
	case "openai":
		if c.OpenAIKey == "" {
			return []string{"OPENAI_API_KEY is required for " + envVar + "=openai"}
		}
		return nil
	case "gemini":
		if c.GeminiKey == "" {
			return []string{"GEMINI_API_KEY is required for " + envVar + "=gemini"}
		}
		return nil
	default:
		return []string{fmt.Sprintf("%s %q: want ollama|openai|gemini", envVar, name)}
	}
}

// validate checks the required keys for the selected backends only — the
// alternates (Azure, OpenAI, Gemini) not chosen anywhere (primary or
// fallback) are not needed for the dev path.
func (c Config) validate() error {
	var errs []string
	if c.TelegramToken == "" {
		errs = append(errs, "TELEGRAM_BOT_TOKEN is required")
	}

	errs = append(errs, c.requireTTSKey(c.TTSBackend, "TTS_BACKEND")...)
	if c.TTSFallbackBackend != "" {
		if c.TTSFallbackBackend == "none" {
			errs = append(errs, "TTS_FALLBACK_BACKEND cannot be none")
		} else {
			errs = append(errs, c.requireTTSKey(c.TTSFallbackBackend, "TTS_FALLBACK_BACKEND")...)
		}
	}

	errs = append(errs, c.requireSTTKey(c.STTBackend, "STT_BACKEND")...)
	if c.STTFallbackBackend != "" {
		if c.STTFallbackBackend == "none" {
			errs = append(errs, "STT_FALLBACK_BACKEND cannot be none")
		} else {
			errs = append(errs, c.requireSTTKey(c.STTFallbackBackend, "STT_FALLBACK_BACKEND")...)
		}
	}

	errs = append(errs, c.requireDialogKey(c.DialogBackend, "DIALOG_BACKEND")...)
	if c.DialogFallbackBackend != "" {
		errs = append(errs, c.requireDialogKey(c.DialogFallbackBackend, "DIALOG_FALLBACK_BACKEND")...)
	}

	if _, err := time.LoadLocation(c.Timezone); err != nil {
		errs = append(errs, fmt.Sprintf("BOT_TIMEZONE %q: %v", c.Timezone, err))
	}

	switch c.SessionStore {
	case "memory", "sqlite":
	default:
		errs = append(errs, fmt.Sprintf("SESSION_STORE %q: want memory|sqlite", c.SessionStore))
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

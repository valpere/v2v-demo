package tts

import "context"

// voiceMap adapts one concrete backend to the bot's abstract voice choice:
// the caller passes "a" or "b" (the conversation's voice, /voice a|b) and each
// backend speaks with ITS OWN configured voice id. Without it a failover from
// one paid backend to another would hand the primary's voice id to the
// fallback (an Azure voice name sent to ElevenLabs is rejected).
type voiceMap struct {
	inner Synthesizer
	a, b  string
}

// WithVoices wraps inner so Speak("…", "a"|"b", …) uses voiceA / voiceB. Any
// other voice selector means A. Put it OUTSIDE a CachedSynth, so the cache key
// carries the real voice id and a changed voice config never replays old audio.
func WithVoices(inner Synthesizer, voiceA, voiceB string) Synthesizer {
	return &voiceMap{inner: inner, a: voiceA, b: voiceB}
}

func (v *voiceMap) Speak(ctx context.Context, text, voice, lang string) ([]byte, error) {
	id := v.a
	if voice == "b" {
		id = v.b
	}
	return v.inner.Speak(ctx, text, id, lang)
}

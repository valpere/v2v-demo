package dialog

import (
	"context"
	"log"
)

// FailoverGenerator tries Primary; on ANY error it retries the same call
// once against Fallback. No transient/permanent error classification —
// none of the Generator impls expose one outside their own package
// (openai_compat.go computes a transient bool internally but never
// exports it), and building that cross-cutting contract has no clear
// payoff at this project's scale. A wasted fallback attempt on a
// genuinely permanent error (a bad API key) costs one extra request,
// which is cheap.
//
// The wrapped backends may already retry once internally on a transient
// failure (openai_compat.go's own retry-on-429/5xx loop) — this stacks
// on top of that, so a real outage costs the primary's full retry budget
// before Fallback is even attempted. Acceptable for an outage path, not
// the common case.
type FailoverGenerator struct {
	Primary, Fallback Generator
}

// Generate implements Generator.
func (f *FailoverGenerator) Generate(ctx context.Context, systemPrompt string, history []Msg) (string, error) {
	text, err := f.Primary.Generate(ctx, systemPrompt, history)
	if err == nil {
		return text, nil
	}
	log.Printf("dialog: primary generator failed, falling back: %v", err)
	return f.Fallback.Generate(ctx, systemPrompt, history)
}

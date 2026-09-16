// Package providermodels asks a provider which models it actually serves.
//
// The app's model catalog was hand-written, and hand-written lists rot: it
// offered gpt-5.1-nano from the open-source release until a live query proved
// OpenAI returns 404 for it. Anything sourced here is a name the provider just
// told us about, under the caller's own key, so "this model does not exist" and
// "your key cannot reach it" stop being indistinguishable guesses.
package providermodels

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Kind is a coarse bucket for a model id. It exists to keep a chat picker from
// offering text-to-speech voices, not to be authoritative — see Classify.
type Kind string

const (
	KindChat       Kind = "chat"
	KindEmbedding  Kind = "embedding"
	KindAudio      Kind = "audio"
	KindImage      Kind = "image"
	KindModeration Kind = "moderation"
	KindCode       Kind = "code"
	KindLegacy     Kind = "legacy"
	KindSnapshot   Kind = "snapshot"
)

// Model is one entry from a provider's catalog.
type Model struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	// RetiresOn is the provider's own shutdown date, empty when it has not set
	// one. Surfaced rather than filtered: a model retiring in three weeks is
	// still usable today, and hiding that fact is how a working setup breaks
	// without warning.
	RetiresOn string `json:"retires_on,omitempty"`
	// Retired is true once RetiresOn has passed. Providers keep returning these.
	Retired bool `json:"retired"`
}

// Lister fetches one provider's catalog using a caller-supplied key.
type Lister interface {
	List(ctx context.Context, apiKey string) ([]Model, error)
}

// Classification is by model id, because that is all most providers give us.
// OpenAI's list endpoint returns an id, a creation timestamp, an owner and a
// shutdown date — no capabilities, no modalities, no display name. So these
// patterns are a filter to keep obvious non-chat entries out of a chat picker,
// and they are wrong at the edges: sora-2 is video and gpt-4o-search-preview is
// a search tool, and both look like chat models by name alone.
//
// That inaccuracy is survivable only because nothing here decides for the user.
// The picker defaults to Chat and offers everything the key returns behind an
// explicit toggle, so a misfiled model costs one checkbox rather than being
// permanently invisible.
var kindPatterns = []struct {
	kind    Kind
	pattern *regexp.Regexp
}{
	{KindEmbedding, regexp.MustCompile(`embedding`)},
	{KindAudio, regexp.MustCompile(`tts|whisper|audio|transcribe|realtime|speech|voice`)},
	{KindImage, regexp.MustCompile(`dall-e|image|sora`)},
	{KindModeration, regexp.MustCompile(`moderation`)},
	{KindCode, regexp.MustCompile(`codex`)},
	{KindLegacy, regexp.MustCompile(`davinci|babbage|curie|instruct`)},
}

// datedSnapshot matches a pinned release like gpt-4o-2024-08-06. These are real
// and callable, but every floating alias has several, so listing them beside
// their alias triples the picker for no gain.
var datedSnapshot = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

// Classify buckets a model id. See kindPatterns for why this is a filter rather
// than a source of truth.
func Classify(id string) Kind {
	low := strings.ToLower(id)
	for _, p := range kindPatterns {
		if p.pattern.MatchString(low) {
			return p.kind
		}
	}
	if datedSnapshot.MatchString(low) {
		return KindSnapshot
	}
	return KindChat
}

// retiredAsOf reports whether a shutdown date has already passed. Providers keep
// listing models past their shutdown date — 17 of OpenAI's 136 on the day this
// was written — so "it came back from the API" is not evidence a model works.
func retiredAsOf(shutdown string, now time.Time) bool {
	if strings.TrimSpace(shutdown) == "" {
		return false
	}
	day, err := time.Parse("2006-01-02", shutdown)
	if err != nil {
		return false
	}
	return day.Before(now.UTC().Truncate(24 * time.Hour))
}

// ErrUnsupportedProvider means this build cannot query that provider's catalog.
// Only OpenAI is implemented; the others are a matter of writing a Lister, not
// of anything structural.
var ErrUnsupportedProvider = errors.New("listing models is not supported for this provider")

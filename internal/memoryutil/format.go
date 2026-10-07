package memoryutil

import (
	"fmt"
	"math"
	"time"

	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// FormatMemoryForContext renders one memory as a context line with a trailing metadata block:
//
//	<content> [stored_at=… confidence=… age_days=… (reconfirmed=Nx) (relevance=…)]
//
// This is the single shared formatter for every load path — prefetch enrichment, the
// find_context() tool, and carry-over — so a memory presents identically regardless of how it
// entered context. The [...] block is stripped on the way back in by StripMemoryContextMetadata.
//
// Every optional signal appears only when it means something, so an ordinary memory reads clean and
// the reader learns "absent field = not notable":
//   - confidence=0.NN — omitted at the DEFAULT anchor (a default carries no signal; emitting it on
//     every memory is noise a reader over-weights). Shown only when actionable: low (be wary),
//     high (trust), or a value refined away from the default.
//   - reconfirmed=Nx  — the dedupe tally, when the same fact has been observed more than once.
//   - relevance=0.NN  — the retrieval-time cosine similarity, set only on freshly-queried memories.
//   - source=external(…) — the memory was learned from someone outside the account in a public
//     Discord relay thread (models.MemoryProvenanceExternal): unverified, and attributed to its
//     speaker when known.
func FormatMemoryForContext(mem *models.Memory) string {
	if mem == nil {
		return ""
	}
	ageDays := int(time.Since(mem.CreatedAt).Hours() / 24)
	meta := fmt.Sprintf("stored_at=%s age_days=%d",
		mem.CreatedAt.UTC().Format(time.RFC3339), ageDays)
	if math.Abs(mem.Confidence-models.DefaultMemoryConfidence) > 1e-9 {
		meta += fmt.Sprintf(" confidence=%.2f", mem.Confidence)
	}
	if mem.ChainMetadata != nil && mem.ChainMetadata.DuplicateCount > 1 {
		meta += fmt.Sprintf(" reconfirmed=%dx", mem.ChainMetadata.DuplicateCount)
	}
	if mem.Relevance != nil {
		meta += fmt.Sprintf(" relevance=%.2f", *mem.Relevance)
	}
	if mem.Provenance.IsExternal() {
		meta += " " + ExternalProvenanceNote(mem.SourceSpeaker)
	}
	return fmt.Sprintf("%s [%s]", mem.Content, meta)
}

// ExternalProvenanceNote is the metadata note an external memory carries in model context: it
// came from someone outside the account, in a Discord thread, and is unverified. The
// speaker is the stored display name (already cleaned of the characters that delimit the
// metadata block), or nil when unknown.
func ExternalProvenanceNote(speaker *string) string {
	name := ""
	if speaker != nil {
		name = models.CleanSpeakerName(*speaker)
	}
	if name == "" {
		return "source=external(unverified; said by someone in a Discord thread, not by the user)"
	}
	return fmt.Sprintf("source=external(unverified; said by %s on Discord, not by the user)", name)
}

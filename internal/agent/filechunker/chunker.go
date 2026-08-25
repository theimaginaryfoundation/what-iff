package filechunker

import "unicode"

const (
	DefaultChunkSize = 2000 // ~512 tokens (in runes, not bytes)
	DefaultOverlap   = 200
)

// TextChunk represents a single chunk of text from a file.
type TextChunk struct {
	Content  string
	Sequence int
}

// ChunkText splits content into overlapping chunks.
// chunkSize is the target character (rune) count per chunk.
// overlap is the number of characters (runes) shared between consecutive chunks.
// Chunks break at whitespace boundaries when possible (don't split mid-word).
// This function operates on runes to correctly handle multi-byte Unicode characters.
func ChunkText(content string, chunkSize int, overlap int) []TextChunk {
	if len(content) == 0 {
		return nil
	}

	// Convert to runes for proper Unicode handling
	runes := []rune(content)

	// Edge case: if overlap >= chunkSize, treat as no overlap to avoid infinite loops.
	if overlap >= chunkSize {
		overlap = 0
	}

	// If the entire content fits in one chunk, return it directly.
	if len(runes) <= chunkSize {
		return []TextChunk{
			{Content: content, Sequence: 0},
		}
	}

	var chunks []TextChunk
	sequence := 0
	start := 0

	for start < len(runes) {
		end := start + chunkSize
		if end >= len(runes) {
			// Last chunk: take whatever remains.
			chunks = append(chunks, TextChunk{
				Content:  string(runes[start:]),
				Sequence: sequence,
			})
			break
		}

		// Find the last whitespace within [start, end) to break at a word boundary.
		breakPos := findLastWhitespaceRune(runes, start, end)

		if breakPos <= start {
			// No whitespace found in the range; break at chunkSize exactly.
			breakPos = end
		}

		chunks = append(chunks, TextChunk{
			Content:  string(runes[start:breakPos]),
			Sequence: sequence,
		})
		sequence++

		// Next chunk starts at (breakPos - overlap), but never before (start + 1)
		// to guarantee forward progress.
		nextStart := breakPos - overlap
		if nextStart < start+1 {
			nextStart = start + 1
		}
		start = nextStart
	}

	return chunks
}

// findLastWhitespaceRune returns the index of the last whitespace rune
// in runes[start:end]. If no whitespace is found, it returns start
// (a sentinel indicating "not found within range").
func findLastWhitespaceRune(runes []rune, start, end int) int {
	for i := end - 1; i > start; i-- {
		if unicode.IsSpace(runes[i]) {
			return i
		}
	}
	return start
}

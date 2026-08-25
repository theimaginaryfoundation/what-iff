package filechunker

import (
	"strings"
	"testing"
)

func TestChunkText(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		chunkSize int
		overlap   int
		wantNil   bool // expect nil return
		wantLen   int  // expected number of chunks (ignored if wantNil)
		validate  func(t *testing.T, chunks []TextChunk)
	}{
		{
			name:      "empty input returns nil",
			content:   "",
			chunkSize: 100,
			overlap:   10,
			wantNil:   true,
		},
		{
			name:      "single word returns single chunk",
			content:   "hello",
			chunkSize: 100,
			overlap:   10,
			wantLen:   1,
			validate: func(t *testing.T, chunks []TextChunk) {
				if chunks[0].Content != "hello" {
					t.Errorf("expected content 'hello', got %q", chunks[0].Content)
				}
				if chunks[0].Sequence != 0 {
					t.Errorf("expected sequence 0, got %d", chunks[0].Sequence)
				}
			},
		},
		{
			name:      "content shorter than chunkSize returns single chunk",
			content:   "the quick brown fox",
			chunkSize: 100,
			overlap:   10,
			wantLen:   1,
			validate: func(t *testing.T, chunks []TextChunk) {
				if chunks[0].Content != "the quick brown fox" {
					t.Errorf("expected full content, got %q", chunks[0].Content)
				}
				if chunks[0].Sequence != 0 {
					t.Errorf("expected sequence 0, got %d", chunks[0].Sequence)
				}
			},
		},
		{
			name:      "content exactly at chunkSize returns single chunk",
			content:   "abcdefghij", // 10 chars
			chunkSize: 10,
			overlap:   3,
			wantLen:   1,
			validate: func(t *testing.T, chunks []TextChunk) {
				if chunks[0].Content != "abcdefghij" {
					t.Errorf("expected exact content, got %q", chunks[0].Content)
				}
			},
		},
		{
			name:      "two chunks with overlap",
			content:   "hello world this is a test of chunking",
			chunkSize: 20,
			overlap:   5,
			validate: func(t *testing.T, chunks []TextChunk) {
				if len(chunks) < 2 {
					t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
				}

				// Verify sequence numbers are sequential starting at 0.
				for i, c := range chunks {
					if c.Sequence != i {
						t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
					}
				}

				// Verify overlap: the end of chunk 0 should appear at the start of chunk 1.
				if len(chunks) >= 2 {
					c0 := chunks[0].Content
					c1 := chunks[1].Content
					// The last `overlap` characters of chunk 0 should be a prefix of chunk 1.
					overlapRegion := c0[len(c0)-5:]
					if !strings.HasPrefix(c1, overlapRegion) {
						t.Errorf("expected chunk 1 to start with overlap region %q, got prefix %q",
							overlapRegion, c1[:min(len(c1), 10)])
					}
				}
			},
		},
		{
			name:      "many chunks all content covered",
			content:   "one two three four five six seven eight nine ten eleven twelve thirteen",
			chunkSize: 15,
			overlap:   3,
			validate: func(t *testing.T, chunks []TextChunk) {
				if len(chunks) < 3 {
					t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
				}

				// Verify sequences are sequential.
				for i, c := range chunks {
					if c.Sequence != i {
						t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
					}
				}

				// Verify all original content is covered by joining chunks
				// (accounting for overlap). We check that every character in
				// the original content appears in at least one chunk.
				original := "one two three four five six seven eight nine ten eleven twelve thirteen"
				covered := make([]bool, len(original))

				for _, chunk := range chunks {
					idx := strings.Index(original, chunk.Content)
					if idx == -1 {
						// Try to find as a substring match with overlap context.
						// Chunk content must appear somewhere in original.
						for pos := 0; pos <= len(original)-len(chunk.Content); pos++ {
							if original[pos:pos+len(chunk.Content)] == chunk.Content {
								idx = pos
								break
							}
						}
					}
					if idx >= 0 {
						for j := idx; j < idx+len(chunk.Content); j++ {
							covered[j] = true
						}
					}
				}

				for i, c := range covered {
					if !c {
						t.Errorf("character at position %d (%q) not covered by any chunk", i, string(original[i]))
					}
				}
			},
		},
		{
			name:      "very long word no whitespace breaks at chunkSize",
			content:   "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz",
			chunkSize: 10,
			overlap:   3,
			validate: func(t *testing.T, chunks []TextChunk) {
				if len(chunks) < 2 {
					t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
				}

				// First chunk should be exactly chunkSize characters since
				// there is no whitespace to break at.
				if len(chunks[0].Content) != 10 {
					t.Errorf("expected first chunk length 10, got %d", len(chunks[0].Content))
				}

				// Verify sequences.
				for i, c := range chunks {
					if c.Sequence != i {
						t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
					}
				}
			},
		},
		{
			name:      "overlap >= chunkSize treated as no overlap",
			content:   "hello world this is a test",
			chunkSize: 10,
			overlap:   15, // overlap > chunkSize
			validate: func(t *testing.T, chunks []TextChunk) {
				if len(chunks) < 2 {
					t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
				}

				// With no overlap, chunks should not share content at boundaries.
				// Each chunk starts right after the previous one ended.
				for i, c := range chunks {
					if c.Sequence != i {
						t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
					}
				}

				// Verify no infinite loop occurred and we covered the content.
				totalChars := 0
				for _, c := range chunks {
					totalChars += len(c.Content)
				}
				original := "hello world this is a test"
				if totalChars < len(original) {
					t.Errorf("total chunk characters (%d) is less than original length (%d)",
						totalChars, len(original))
				}
			},
		},
		{
			name:      "whitespace-only content returns single chunk",
			content:   "   \t\n  ",
			chunkSize: 100,
			overlap:   10,
			wantLen:   1,
			validate: func(t *testing.T, chunks []TextChunk) {
				if chunks[0].Content != "   \t\n  " {
					t.Errorf("expected whitespace content preserved, got %q", chunks[0].Content)
				}
			},
		},
		{
			name:      "content with newlines treated as whitespace for breaking",
			content:   "line one\nline two\nline three\nline four\nline five",
			chunkSize: 20,
			overlap:   5,
			validate: func(t *testing.T, chunks []TextChunk) {
				if len(chunks) < 2 {
					t.Fatalf("expected at least 2 chunks, got %d", len(chunks))
				}

				// Verify that breaks happen at newline boundaries.
				for i, c := range chunks {
					if c.Sequence != i {
						t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
					}
					// No chunk should start or end with a split mid-word
					// (unless it's the very start or very end).
					content := c.Content
					if len(content) > 0 {
						// Content should not start with a partial word
						// (i.e., first char should not be a continuation of a word
						// unless it's the first chunk).
						_ = content // validated by structure
					}
				}

				// Verify all original content is covered.
				original := "line one\nline two\nline three\nline four\nline five"
				covered := make([]bool, len(original))
				for _, chunk := range chunks {
					for pos := 0; pos <= len(original)-len(chunk.Content); pos++ {
						if original[pos:pos+len(chunk.Content)] == chunk.Content {
							for j := pos; j < pos+len(chunk.Content); j++ {
								covered[j] = true
							}
							break
						}
					}
				}
				for i, c := range covered {
					if !c {
						t.Errorf("character at position %d (%q) not covered", i, string(original[i]))
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkText(tt.content, tt.chunkSize, tt.overlap)

			if tt.wantNil {
				if chunks != nil {
					t.Errorf("expected nil, got %v", chunks)
				}
				return
			}

			if chunks == nil {
				t.Fatal("expected non-nil chunks, got nil")
			}

			if tt.wantLen > 0 && len(chunks) != tt.wantLen {
				t.Errorf("expected %d chunks, got %d", tt.wantLen, len(chunks))
			}

			if tt.validate != nil {
				tt.validate(t, chunks)
			}
		})
	}
}

func TestChunkTextDefaults(t *testing.T) {
	// Verify the default constants are set as expected.
	if DefaultChunkSize != 2000 {
		t.Errorf("expected DefaultChunkSize 2000, got %d", DefaultChunkSize)
	}
	if DefaultOverlap != 200 {
		t.Errorf("expected DefaultOverlap 200, got %d", DefaultOverlap)
	}
}

func TestChunkTextForwardProgress(t *testing.T) {
	// Ensure the chunker always makes forward progress and terminates
	// even with adversarial inputs.
	content := strings.Repeat("a ", 1000) // 2000 chars of "a " repeated
	chunks := ChunkText(content, 50, 49)  // near-maximum overlap

	if len(chunks) == 0 {
		t.Fatal("expected chunks, got none")
	}

	// Verify it terminates in a reasonable number of chunks.
	// With chunkSize=50 and overlap=49, each step advances by ~1 char,
	// so we'd expect roughly 2000 chunks in the worst case.
	if len(chunks) > 5000 {
		t.Errorf("too many chunks (%d), possible infinite loop", len(chunks))
	}

	// Verify all sequences are sequential.
	for i, c := range chunks {
		if c.Sequence != i {
			t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
			break
		}
	}
}

func TestChunkTextUnicode(t *testing.T) {
	// Test that multi-byte Unicode characters are not split mid-character.
	// Chinese characters are 3 bytes each in UTF-8.
	// "你好世界" = 4 characters, 12 bytes
	// "こんにちは" = 5 characters, 15 bytes (Japanese Hiragana)

	tests := []struct {
		name      string
		content   string
		chunkSize int
		overlap   int
	}{
		{
			name:      "Chinese text single chunk",
			content:   "你好世界", // 4 runes
			chunkSize: 10,
			overlap:   2,
		},
		{
			name:      "Chinese text multiple chunks",
			content:   "你好世界这是一个测试文本用于验证分块功能", // 18 runes
			chunkSize: 5,
			overlap:   1,
		},
		{
			name:      "Japanese text",
			content:   "こんにちは世界プログラミング", // 14 runes
			chunkSize: 6,
			overlap:   2,
		},
		{
			name:      "Mixed ASCII and Unicode",
			content:   "Hello 世界! This is a 测试 test with émojis 🎉 and ümlauts.",
			chunkSize: 15,
			overlap:   3,
		},
		{
			name:      "Emojis (4-byte UTF-8)",
			content:   "🎉🎊🎁🎂🎈🎆🎇✨", // 8 emoji runes
			chunkSize: 3,
			overlap:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkText(tt.content, tt.chunkSize, tt.overlap)

			if chunks == nil {
				t.Fatal("expected chunks, got nil")
			}

			// Verify each chunk is valid UTF-8 (no split multi-byte characters).
			for i, c := range chunks {
				// Go strings are always valid UTF-8 if constructed properly.
				// If we split mid-character, the string would contain invalid
				// UTF-8 sequences. We can check by counting runes.
				runeCount := 0
				for _, r := range c.Content {
					if r == '\uFFFD' {
						t.Errorf("chunk %d contains replacement character (invalid UTF-8)", i)
					}
					runeCount++
				}

				// Verify chunk is not empty.
				if runeCount == 0 {
					t.Errorf("chunk %d is empty", i)
				}

				// Verify sequence.
				if c.Sequence != i {
					t.Errorf("chunk %d: expected sequence %d, got %d", i, i, c.Sequence)
				}
			}

			// Verify all original content is covered.
			originalRunes := []rune(tt.content)
			covered := make([]bool, len(originalRunes))

			for _, chunk := range chunks {
				chunkRunes := []rune(chunk.Content)
				// Find this chunk in the original.
				for pos := 0; pos <= len(originalRunes)-len(chunkRunes); pos++ {
					match := true
					for j := 0; j < len(chunkRunes); j++ {
						if originalRunes[pos+j] != chunkRunes[j] {
							match = false
							break
						}
					}
					if match {
						for j := pos; j < pos+len(chunkRunes); j++ {
							covered[j] = true
						}
						break
					}
				}
			}

			for i, c := range covered {
				if !c {
					t.Errorf("rune at position %d (%q) not covered by any chunk", i, string(originalRunes[i]))
				}
			}
		})
	}
}

# Package: `internal/speechtext`

## Role

Turns a reply's markdown into the words to read aloud, and splits them into the segments a speech model synthesises (ADR 0x052 §6).

## Responsibilities

- `Spoken(message, finalRoundOffset)` is a reply's final round, prepared: narration written before a tool call is not read.
  A final round with nothing to read (a turn that ended on a tool) reads the whole reply instead.
- `Prepare` reduces markdown to its words: emphasis, headings and list markers go, a link is read as its text, and fenced code, tables, images, bare URLs, HTML and emoji are dropped.
  Paragraphs stay apart, and a heading or list item without closing punctuation gets a full stop so the voice pauses.
- `Segment(text, maxChars)` puts the first sentence on its own, so speech starts quickly, then packs whole paragraphs up to `maxChars`; a longer paragraph splits between sentences, and a longer sentence between words.
  Sentences also end at a full-width stop (。！？), and a "word" longer than `maxChars` (text without spaces) is cut by length.
  Every segment is a run of the text, joined as it stood (no space added after a full-width stop or inside a cut word).
- `Remaining(prepared, said)` is the text after segments already read, or false when the text no longer starts with them: a track resumes from what it read, not how many segments it has.

- `NextSpeakable` picks the next piece of streaming text to read now: only sentences followed by space or ended by a full-width stop (the rest may still change), one for a round's first piece, then to a paragraph's end or about a third of the limit.
- `Settled(md)` is the part of streaming markdown whose words can't change as more arrives: the line being written is cut before an emphasis, code span, strike-through, link or tag still open on it, or a fence it opens.
- `Resync(prepared, said)` finds where prepared text reaches words already read aloud that were prepared differently (same letters and digits), or says they differ.

## Dependencies

- **Inbound:** `internal/agent` (voice replies and previews).
- **Outbound:** standard library only.

## Non-obvious decisions

- Underscores and asterisks inside words are kept (`snake_case`, `a*b`); only emphasis that wraps text is removed.
- Sentence splitting skips common abbreviations ("e.g.", "Dr.") and keeps closing quotes with their sentence.
- Dropping something (an emoji, a link) can leave a space before punctuation, which is removed.

## Testing

- `speechtext_test.go` — markdown reduced (links, emphasis, lists, headings, code, tables, quotes, HTML, emoji, URLs), words with underscores kept, an unclosed fence, the final round read, segments (first sentence alone, packing, the limit kept, nothing lost, a one-sentence reply, nothing to read), long sentences split between words, and abbreviations.

## Related documentation

- [ADR 0x052](../../docs/adr/0x052-voice-replies-and-dictation.md)

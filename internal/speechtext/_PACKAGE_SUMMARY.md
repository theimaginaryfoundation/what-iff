# Package: `internal/speechtext`

## Role

Turns a reply's markdown into the words to read aloud, and splits them into the segments a speech model synthesises (ADR 0x052 §6).

## Responsibilities

- `Spoken(message, finalRoundOffset)` is a reply's final round, prepared: narration written before a tool call is not read.
- `Prepare` reduces markdown to its words: emphasis, headings and list markers go, a link is read as its text, and fenced code, tables, images, bare URLs, HTML and emoji are dropped.
  Paragraphs stay apart, and a heading or list item without closing punctuation gets a full stop so the voice pauses.
- `Segment(text, maxChars)` puts the first sentence on its own, so speech starts quickly, then packs whole paragraphs up to `maxChars`; a longer paragraph splits between sentences, and a longer sentence between words.

- `NextSpeakable` picks the next piece of streaming text to read now: only sentences followed by space (the rest may still change), one for a round's first piece, then to a paragraph's end or about a third of the limit.

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

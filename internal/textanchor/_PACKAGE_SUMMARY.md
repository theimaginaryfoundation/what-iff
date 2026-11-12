# Package: `internal/textanchor`

## Role

Keeps ranges of a text on the same words as the text changes (ADR 0x045): the anchors of comments on text and code files, which have no place of their own for comments.

## Responsibilities

- **`Remap(old, new, ranges)`** carries each range from the old text to the new one, or returns nil for a range none of whose words are left.
  An edit inside a range stays inside it; a changed start moves to the first word after it that is still there; a changed end moves to the last one before it.
- **`Units` and `Slice`** convert text to UTF-16 code units (without a leading byte order mark) and quote a range of them.

## Dependencies

- **Inbound:** `internal/filecomments`.
- **Outbound:** the standard library only.

## Non-obvious decisions

- **Word by word, never whitespace on its own.**
  Tokens are runs of letters, digits and underscores, and single other characters; whitespace is skipped.
  A character diff matches stray spaces and letters, which pulls a range onto text it was not on.
- **Offsets are UTF-16 code units**, as the browser and the Monaco editor count them, so the client and the server name the same text.
- **Myers' diff with a bound.**
  The common head and tail are matched first, and the search keeps only the diagonals each step reads back.
  Past `MaxEdits` changed words, or past `maxSteps` of work (a large file with many edits), a range is found by its own text, nearest to where it was, which is what a rewrite of the whole file comes down to.
  Token values are numbered once (a vocabulary shared by both texts), so the diff compares integers and each value is held once.
- **A range that starts or ends inside a kept word keeps that part of the word.**

## Testing

- `textanchor_test.go` — edits inside, before and after a range, a range inside a word, a range whose words are all gone, code edits, UTF-16 counting with emoji and a byte order mark, the fallback past the diff's bound and past its work budget (`maxSteps`, lowered in the test); `BenchmarkRemapLargeFile` times a 4 MB file edited throughout (about 0.1 s), kept out of the race-detector test run where wall time is meaningless.

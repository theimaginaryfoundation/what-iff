# Package: `internal/filecomments`

## Role

Keeps the comments on text and code files (ADR 0x045), which have no place for comments of their own: a file's threads are stored beside it (ent `FileCommentSet`) with the text their anchors are placed against, and `GET`/`PUT /file-attachment/{id}/comments` only map errors to statuses.

## Responsibilities

- **`Service.Read`** returns the file's threads placed on its current content.
  When the file changed since they were stored (a save, an agent's file tools, anything that bumps `content_version`), it carries every anchor to the new text with `internal/textanchor`, stores the result, and returns it; the next read is direct.
- **`Service.Write`** stores threads placed on `content_version`, which must be the file's current content (`ErrStaleContent` otherwise), over the revision the caller read (`ErrRevision` otherwise).
  It reads the file's text itself as the new baseline, so a client cannot store anchors against text the file does not hold.
- **`Place`** checks anchors against the text and sets each thread's quote; **`Carry`** moves anchors to new text.
- **Refusals:** `ErrNotText` for content that is not UTF-8 or is over `MaxTextBytes` (4 MB).

## Dependencies

- **Inbound:** `internal/handlers/fileattachment`.
- **Outbound:** `internal/datastore` (`GetFileAttachment`, `GetFileCommentSet`, `PutFileCommentSet`), `internal/storage` (`ReadAttachment`), `internal/textanchor`.

## Non-obvious decisions

- **Anchors are carried on read, not on write.**
  Whoever changes a file, the comments are carried when they are next needed, against the baseline text kept with them; no writer has to know about comments.
- **A thread whose words are all gone is not dropped.**
  Its anchor becomes null (a comment on the whole file) and it keeps the quote it had, so the reader still sees what it was about.
- **Two reads carrying at once:** the second one's write fails on the revision, and it reads what the first stored.

## Testing

- `filecomments_test.go` — placing and quoting on write, stale content and revision refusals, carrying across a change made elsewhere (including the start and end moving), detaching once the words are gone, a file with no comments, and binary content.

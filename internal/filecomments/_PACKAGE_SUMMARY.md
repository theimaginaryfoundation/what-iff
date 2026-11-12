# Package: `internal/filecomments`

## Role

Keeps the comments on text, code and image files (ADR 0x045), which have no place for comments of their own: a file's threads are stored beside it (ent `FileCommentSet`) with the text their anchors are placed against, and `GET`/`PUT /file-attachment/{id}/comments` only map errors to statuses.

## Responsibilities

- **`Service.Read`** returns the file's threads placed on its current content.
  When the file changed since they were stored (a save, an agent's file tools, anything that stores a new object), it carries every anchor to the new text with `internal/textanchor`, stores the result, and returns it; the next read is direct.
- **`Service.Write`** stores threads placed on `content_version`, which must be the file's current content (`ErrStaleContent` otherwise), over the revision the caller read (`ErrRevision` otherwise).
  It reads the file's text itself as the new baseline, so a client cannot store anchors against text the file does not hold.
  It also sets authorship (`keepAuthorship`): a comment already stored keeps its author and time, and its text unless the user wrote it; a new one is the user's (`user:<id>`), dated by the server when the client's time is missing or ahead.
- **`Place`** checks anchors against the text and sets each thread's quote; **`Carry`** moves anchors to new text.
- **One set per file, whichever row it is read through.**
  A chat copy of a file reads and writes the original's set (`CommentFileID`), so a comment made in a chat shows in the gallery and the other way round.
- **Images:** every thread is on the whole file (`anchor` null, no quote); no text is read and an edit leaves the threads as they are.
- **Refusals:** `ErrNotText` for content that is neither an image nor UTF-8 text, or text over `MaxTextBytes` (4 MB).
  A read of a file that stopped being such text returns its threads on the whole file, unstored, so they stay readable; a text file over the limit also takes writes of threads on the whole file (`errTextTooLarge`).
  A row with no key (from before keys were recorded) reads a missing or empty object as empty text, as it always did.
  `ErrContentMissing` when the object the row names is not there (503); a read retries it, since a save that replaced the object mid-read is the usual cause, and a write turns it into `ErrStaleContent` when the row moved on.

## Dependencies

- **Inbound:** `internal/handlers/fileattachment`.
- **Outbound:** `internal/datastore` (`GetFileAttachment`, `CommentFileID`, `GetFileCommentSet`, `PutFileCommentSet`), `internal/storage` (`DownloadFile`, `ReadAttachment` for rows with no key), `internal/textanchor`.

## Non-obvious decisions

- **Anchors are carried on read, not on write.**
  Whoever changes a file, the comments are carried when they are next needed, against the baseline text kept with them; no writer has to know about comments.
- **A thread whose words are all gone is not dropped.**
  Its anchor becomes null (a comment on the whole file) and it keeps the quote it had, so the reader still sees what it was about.
- **The baseline is the stored object, not a version number.**
  Every save writes a new object, and a file's rows each count their own `content_version`, so the set records the `s3_key` its baseline was read from; a different key on the row read through is changed content.
  A row stored before keys were recorded uses the key it is read from (`FileKeyForAttachment`), which is also the key its chat copies were given, so reading through either does not carry again.
- **A missing object is never empty text.**
  `DownloadFile` is `(nil, nil)` for a missing key; read as `""`, a carry would put every anchor nowhere and store that as the baseline for good.
  Only the object the row names is read, not the legacy fallback keys, which could hold an older version.
- **Image regions are left for later, and not ruled out.**
  A box drawn on an image would be a separate field of the thread, in fractions of the image's size; the stored `s3_key` baseline already tells a read that the image was edited, which is when regions would have to move.
- **Two reads carrying at once:** the second one's write fails on the revision, and it reads what the first stored.

## Testing

- `filecomments_test.go` — a missing object mid-read (retried, never carried onto empty text), a file that stopped being text, server-set authorship, placing and quoting on write, stale content and revision refusals, carrying across a change made elsewhere (including the start and end moving), detaching once the words are gone, a file with no comments, binary content, a file and its chat copy sharing one set across an agent's edit, and an image's whole-file threads outliving an edit.

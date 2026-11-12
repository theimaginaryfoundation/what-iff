# Package: `internal/filecomments`

## Role

Keeps the comments on text, code and image files (ADR 0x045), which have no place for comments of their own: a file's threads are stored beside it (ent `FileCommentSet`) with the text their anchors are placed against, and `GET`/`PUT /file-attachment/{id}/comments` only map errors to statuses.

## Responsibilities

- **`Service.Read`** returns the file's threads placed on its current content.
  When the file changed since they were stored (a save, an agent's file tools, anything that stores a new object), it carries every anchor to the new text with `internal/textanchor`, stores the result, and returns it; the next read is direct.
- **`Service.Write`** stores threads placed on `content_version`, which must be the file's current content (`ErrStaleContent` otherwise), over the revision the caller read (`ErrRevision` otherwise).
  It reads the file's text itself as the new baseline, so a client cannot store anchors against text the file does not hold.
- **`Place`** checks anchors against the text and sets each thread's quote; **`Carry`** moves anchors to new text.
- **One set per file, whichever row it is read through.**
  A chat copy of a file reads and writes the original's set (`CommentFileID`), so a comment made in a chat shows in the gallery and the other way round.
- **`Update`** (`agent.go`, ADR 0x046) reads a file's threads with the text they are on, applies a change and writes it, again once when the file or comments changed in between; **`AnchorOn`** places a thread by quoting the text (`ErrQuoteNotFound`, `ErrQuoteAmbiguous`) and **`Lines`** names a range's lines.
- **`Office`** (`office.go`, ADR 0x046) reads and changes the comments inside Word, PowerPoint and Excel files through the `internal/officecomments` formats, by extension.
  A change is a new version of the file, saved through `filesave.Service.Replace` over the version read and started again once when someone saved meanwhile (`ErrStaleContent` after that).
- **`MentionContext`** (`mention.go`, ADR 0x046) renders the context a comment that mentions the agent sends with its message: the file, what the thread is on and the thread before the comment, bounded in length; `ShortQuote` is the quote a link to the thread shows.
- **Images:** every thread is on the whole file (`anchor` null, no quote); no text is read and an edit leaves the threads as they are.
- **Refusals:** `ErrNotText` for content that is neither an image nor UTF-8 text, or text over `MaxTextBytes` (4 MB).

## Dependencies

- **Inbound:** `internal/handlers/fileattachment`.
- **Outbound:** `internal/datastore` (`GetFileAttachment`, `CommentFileID`, `GetFileCommentSet`, `PutFileCommentSet`), `internal/storage` (`ReadAttachment`), `internal/textanchor`.

## Non-obvious decisions

- **Anchors are carried on read, not on write.**
  Whoever changes a file, the comments are carried when they are next needed, against the baseline text kept with them; no writer has to know about comments.
- **A thread whose words are all gone is not dropped.**
  Its anchor becomes null (a comment on the whole file) and it keeps the quote it had, so the reader still sees what it was about.
- **The baseline is the stored object, not a version number.**
  Every save writes a new object, and a file's rows each count their own `content_version`, so the set records the `s3_key` its baseline was read from; a different key on the row read through is changed content.
  A row with no key (content inline) uses its id and version instead.
- **Image regions are left for later, and not ruled out.**
  A box drawn on an image would be a separate field of the thread, in fractions of the image's size; the stored `s3_key` baseline already tells a read that the image was edited, which is when regions would have to move.
- **Two reads carrying at once:** the second one's write fails on the revision, and it reads what the first stored.

## Testing

- `filecomments_test.go` — placing and quoting on write, stale content and revision refusals, carrying across a change made elsewhere (including the start and end moving), detaching once the words are gone, a file with no comments, binary content, a file and its chat copy sharing one set across an agent's edit, and an image's whole-file threads outliving an edit.

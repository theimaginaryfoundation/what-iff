# Package: `internal/officecomments`

## Role

Reads and writes the comments inside Word, PowerPoint and Excel files on the server (ADR 0x046), so an agent can take part in them as the browser's editors do (ADR 0x045 §2).
Each format works on the file's bytes, writes what the browser writes, and leaves the rest of the package as it was.

## Responsibilities

- **`officecomments`** — the shared `Format` interface (`List`, `Add`, `Reply`, `Resolve` over a file's bytes), `Thread`, `Comment`, `Anchor`, the errors the agent's tool turns into refusals, and how people are named in files (`PersonID`, `ProviderUserID`).
  `limits.go`: `Budget` reads zip entries within `MaxPartBytes` each and `MaxReadBytes` together, `MaxXMLDepth` caps nesting (`ErrTooLarge`), and `XMLText` drops the characters XML cannot hold, which every format's escaper applies.
  `Budget.ReadLarge` reads a part past `MaxPartBytes` within the total (an xlsx worksheet a note is linked from); `NotOffice` keeps a depth refusal `ErrTooLarge`.
  The pptx and xlsx packages record the first part they could not read or parse and fail the change on it (`pkg.err`), never taking it for a missing part; pptx caches the parts it read.
  `officecommentstest.WellFormed` checks a package's parts with a strict parser, for the formats' tests.
- **`docx`** — Word comments: comments.xml with commentsExtended, commentsIds, commentsExtensible and people.xml.
  Thread and comment ids are Word's durable ids, as the browser reads them.
  Add places a thread on quoted text in the body, splitting runs at its ends and writing the range markers and reference as the browser does.
- **`pptx`** — PowerPoint modern comments (p188) and authors.xml.
  Add places a thread on quoted text in a top-level shape, with the moniker list, `ac:context` hash and `a16:creationId` PowerPoint checks, or on a slide.
  A legacy thread is rewritten in the modern form when it is first changed, as the browser does.
- **`xlsx`** — Excel threaded comments with the persons part, plus the legacy note placeholder and VML shape Excel needs.
  Add takes a cell (one thread per cell); a thread is labelled `Sheet!B3`.

## Dependencies

- **Inbound:** `internal/filecomments` (`Office`), `internal/agent` (`officeCommentFormats`).
- **Outbound:** standard library and `github.com/google/uuid` only.

## Non-obvious decisions

- **Edits, not rewrites.**
  XML parts are edited as text at offsets an `encoding/xml` Decoder finds; nothing is marshalled back, which would change namespace prefixes.
  Untouched zip entries are copied raw in their original order.
- **People.**
  A WhatIff user or agent is a person with the provider `WhatIff` and the user's id, or an agent's whole `agent:<id>`, as its user id; the browser maps them the same way.
- **Known differences from a browser save** (meaning the same, bytes not identical):
  - Word renumbers `w:id` from the highest one rather than from 0.
  - PowerPoint edits a slide's comment part in place rather than regenerating it.
  - Excel writes `dT` in UTC where the browser writes local time without a zone.
  - Excel refuses a new thread on a cell with a plain note.

## Testing

- Each format builds small packages in its tests: listing what the browser writes, Add, Reply and Resolve round trips, refusals, an agent author surviving, and untouched entries byte-identical.
- Hostile input: control characters in comments and names (output checked with a strict parser), parts nested past the limit, a slide with no shape tree, a broken comment part (an error, not a second part), and a Word 2007 comment (`ErrOldComment`); `limits_test.go` covers the read budget.
- `pptx` checks the context hash against the browser's vectors.

# ADR 0x045: Document comments

- **Status:** Accepted
- **Date:** 2026-11-12
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

The file workspace (ADR 0x043) lets a user view and edit documents, slides, spreadsheets, text, code and images.
Working on a file with someone else, whether a person or an agent, needs comments: a remark on a sentence, a cell, a shape or a line of code, a reply to it, and a way to mark it done.

Word, PowerPoint and Excel files have comments of their own, so comments made here should be those comments, and should open in Office where they were made.
Text, code and image files have no place for comments, so they have to be kept somewhere else, and still stay on the text they are about while the file is edited, by the user or by an agent.

This ADR covers comments made by people.
Agents reading, writing and being @-mentioned in comments are a later step; nothing here is specific to people beyond who the author is.

## Decision

### 1. One comments panel for every format

Every format shows its comments in the same panel (`features/office/comments`).
A thread is its first comment and its replies, anchored on something in the file or on the whole file, and can be resolved.
A comment can @-mention people: the mention is written into the text as `@Name` and also listed with the comment (id and name), so a later step can act on mentions without parsing text.
The author of a comment made here is `user:<id>` with the user's display name.
Where the server keeps the comments (section 4) it sets who wrote each one: a comment it has already stored keeps its author, time and (if someone else wrote it) text, and a new one is the user's.

Choosing a thread in the panel makes it active in the page, and choosing commented content in the page makes its thread active in the panel.
Every format has the same right-click items: *New comment* on content that has none, and *Reply to comment* and *Delete comment* on content that has one.

### 2. Word, PowerPoint and Excel: comments live in the file

- **Word.** Comments are Univer custom decorations over the commented text, painted by an interceptor, and are read from and written to `comments.xml` with `commentsExtended.xml` (replies and resolution), `commentsIds.xml`, `commentsExtensible.xml` and `people.xml`.
  People made here are written with the provider `WhatIff` and their `user:<id>`.
  A comment keeps its `w:id`, which content the editor keeps as it was (a text box, a tracked deletion) still marks it by, and new comments are numbered above the file's.
  Comments with no place in the text the editor shows (a footnote's, a text box's) are not shown and are written back as they were.
- **PowerPoint.** Comments are modern comments (`p188`) with `authors.xml`.
  A comment is on a slide, on a shape (by the shape's `a16:creationId`, given one if it has none), or on a range of a shape's text.
  A text range carries the moniker list and the `ac:context` hash PowerPoint checks (the MS-ODRAWXML hash of the shape's text, each paragraph ended by `\r`); without it PowerPoint shows the comment on the shape.
  Nothing keeps a range on its words while the text is edited, so a save finds the words again (nearest the old place) and writes the range and hash anew whenever the shape's text changed; a range whose words are gone becomes a comment on the shape.
  Text ranges are painted with the CSS Custom Highlight API, so the slide's text is never changed to mark them.
- **Excel.** Comments are threaded comments on a cell, carried by the existing Univer resource pipeline, with a marker painted in the cell.
  The cell is tagged with its thread; copying the cell copies the tag, so the thread stays on the cell it was on and the copies lose it.
  Univer's own thread-comment preset is not used: its UI does not fit the shared panel.

### 3. Commenting while viewing

A user who may change a file can comment on it without entering edit mode.
For Word, PowerPoint and Excel the comment is part of the file, so the file is saved with each comment as soon as it is made (`CommentSaver`, provided by the workspace); like every save, it replaces the file's content and keeps no earlier version (ADR 0x043).
A save that would lose content asks first, once per file; a version written elsewhere in the meantime reloads the file rather than overwrite it.

### 4. Text, code and images: comments kept beside the file

A file's comments are stored in a `file_comment_sets` row beside it, read and written with `GET` and `PUT /file-attachment/{id}/comments`.
A write sends the `content_version` its anchors are placed on (412 when that is not the file's content) and the revision it read (409 when the comments changed since).
On a 409 the client reads the comments again, merges its changes with theirs and writes again; on a 412 the viewer reloads the file.

On text, a thread is on a range of the text in UTF-16 code units, so the browser's string offsets can be used as they are.
The set keeps the text the anchors were placed on, and the stored object (`s3_key`) that text came from.
When a read finds the file's object changed, whoever changed it, it carries every anchor to the new text and stores the result, so no writer has to know about comments.
An object the row names that cannot be found (a save replaced it during the read) is an error, never empty text, which would put every anchor nowhere.
A text file past 4 MB keeps its comments on the whole file: they can be read and written there, though not on ranges of its text.

Carrying follows Word: a range keeps its words.
The old and new text are compared word by word (a Myers diff, bounded in edits and in work, so neither a large rewrite nor a large file costs more than a fraction of a second; past the bound a range is found by its words).
A range whose middle was edited keeps both ends; one whose first or last words were removed shrinks to the first and last words left; one with no words left becomes a comment on the whole file and keeps the quote it had, so the reader still sees what it was about.

The editor shows threads as Monaco decorations that follow typing, and the viewer as highlights over the text.
Formatted Markdown, in the viewer and in the editor's live preview, does not match the source character for character, so a range is found there by its words, nearest the place it is in the source; a thread on something not shown (a link's address) switches the viewer to the raw text.
Comments made in the editor are written after a save, placed on what the save stored; comments made in the viewer are written as they are made.

On an image, every thread is on the whole image for now: the server drops an anchor sent for one and reads no bytes, and an edit leaves the threads as they are.
Comments kept beside a file only need the user to be allowed to change it, so they can be written on images no editor can save (a GIF, an SVG).

### 5. A file and its chat copies have one set of comments

Using a gallery file in a chat makes a row that shares its stored object (a chat copy).
A copy records the file it copies (`original_id`); a copy of a copy records the same original.
Copies made before the link existed are linked the first time their comments are read, to the earliest row on the same object that is not context-only.
Comments are read and written through the original, so a comment made in a chat shows in the gallery and the other way round.

The baseline is the stored object rather than a version number, because each row counts its own `content_version`.
For the same reason, an agent's edit of its file now moves the file's chat copies with it, as a user's save already did; context-only rows (what a reply showed) keep the version they had.
Deleting an original hands its copies, and its comments, to the earliest copy left; so does deleting the chat, personality or skill that holds it, before the cascade removes it, whether or not it has comments yet.
A copy that came to hold comments after its original went without handing over (before this was done) hands them to its sibling copies, joining any set they hold on the whole file.

## Consequences

- Comments made here open in Word, PowerPoint and Excel where they were made, and theirs open here.
- Text and code comments survive edits made anywhere, including by an agent's file tools, without those tools changing.
- Every comment made while viewing an Office file saves the file again, with no earlier version kept.
- A file's comments are the same wherever it is opened.

## Later

Agents taking part in comments, and editing a file they are mentioned on, are ADR 0x046.

- **Regions of an image.** A box drawn on an image would be a separate field of the thread, in fractions of the image's width and height so it does not depend on the image's size; an edit that changes the image's geometry (a crop, a rotation) would move it or put the thread on the whole image, which the stored-object baseline already tells a read about.

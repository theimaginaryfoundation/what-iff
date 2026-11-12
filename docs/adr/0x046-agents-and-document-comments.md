# ADR 0x046: Agents and document comments

- **Status:** Accepted
- **Date:** 2026-11-12
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

People comment on files in every format (ADR 0x045).
The point of comments here is working on a file together with an agent: the user comments on a file, the agent changes the file and answers in the thread, the user answers back.
That needs four things comments alone do not give: a way to bring a comment to the agent, a way for the agent to read and write comments, a way to see in the chat what the agent did in the file, and permission for the agent to change a file the user, not the agent, made (ADR 0x034 lets agents change only their own files).

Word, PowerPoint and Excel comments live inside the file, and until now only the browser could read or write them.

## Decision

### 1. Mentioning the agent sends it the comment

In a chat, the chat's agent can be mentioned in a comment on any file open there, in every format.
It is a person in the mention list as `agent:<personality id>` (`agent:chat` with no personality), under the personality's name.

Posting a comment or reply that mentions it sends the agent a message of the chat (`POST /chat/{id}/chat-message` with `comment`), once the save that stores the comment lands: the agent then reads the thread it is asked about, and its own change to the file does not race the user's save.
While viewing, that is as soon as the comment is saved; while editing, when the user saves the file.
A save that fails keeps the mention for the next one, and one that throws the comment away (the file was reloaded) drops it.
The send goes through the chat's send gate, like a message (a blocked send says so), and to the chat the comment was posted in, even if another is shown by the time the save lands.

- The message's text is the comment's text.
  The composer's draft is left as it was; while a reply is running the message is a follow-up to it (ADR 0x042), and the running turn takes in its context with its text, clipped so the user's own words and the follow-up's framing always fit.
- What the comment is about goes with the message as context the chat does not show: the file, what the thread is on (as the comments panel quotes it) and the thread before the comment.
  It is a `FILE_COMMENT` context item, rendered in that message's own turn after its text, now and whenever the message is in history, not merged with the conversation's other context.
  The rewrite of a message's context after inference keeps it.
- The message shows a link under its text, "on plan.md · "major features"" (`comment_refs`), which opens the file at the thread.
  `?comment=<thread id>` beside `?file=` is that link; the comments panel opens and the thread is chosen as if clicked, in every format.

The client sends the thread as the panel shows it, so the agent sees what the user saw, and the server checks only that the file is the user's.
The context is bounded (the last 30 comments, each clipped).

### 2. The agent reads and writes comments

The `file_comments` tool lists a file's threads, replies to one, starts one, and resolves or reopens one, on any of the user's files, outside sandboxed chats.
On text, code and images a comment is kept beside the file, not in it, so writing one needs no edit permission.
In Word, PowerPoint and Excel a comment is part of the file and writing one writes a new version of it, so it needs the grant of §4; listing does not.
The agent writes as `agent:<personality id>` under the personality's name.

- **Text and code.** A new thread is placed by quoting the file's text exactly; a quote found more than once needs an occurrence, and one not found is refused, so the agent never deals in offsets.
  `list` gives each thread's quote and lines.
  Every change reads the threads with the file's text, applies the change and writes it, starting again once when the file or its comments changed meanwhile (`filecomments.Service.Update`).
- **Images.** Threads are on the whole image.
- **Word, PowerPoint, Excel.** See §5. Word places a thread on quoted text; PowerPoint on quoted text in a shape (optionally on one slide) or on a slide; Excel on a cell, one thread per cell.
- Comments are plain text; the tool asks for short notes without Markdown.

### 3. The reply shows what the agent did

Each comment the agent made or replied to in a turn is a `comment_ref` on its reply, one per thread, taken from the turn's `file_comments` calls.
The chat shows them as pills, like attached files, that open the file at the thread.

There is no push channel to the browser.
When a reply that commented on the file open in the workspace lands, the workspace reloads the file as stored and its comments with it, unless the user has unsaved work in it.

### 4. Mentioning the agent on a file lets it edit the file

A comment that mentions the agent, sent from a chat, grants that chat's agent the file (`file_edit_grants`).
The grant is on the file's original row, so it covers the file's chat copies, and it goes with the file or the chat.
Other chats' agents, and the same personality in another chat, are not granted it.
The grant is for the personality the chat had when it was made: switching the chat to another personality does not pass it on.
A send that fails puts the grant back as it was: removed if the send made it, or given back to the personality it had.

A grant is used only in a turn the user started in the app.
A job, a webhook message or a relayed message (a Discord thread) runs in the same chat but is not the user asking, and a prompt injected there must not be able to rewrite the user's files.

The file workspace says when the chat's agent may change the open file, with a Revoke button; `GET /chat/{id}/file-edit-grants` lists a chat's grants, and `GET` and `DELETE /chat/{id}/file-edit-grants/{file id}` read and revoke one.

With the grant, the agent's file tools may change the file's text (append, patch), and `agent_file_read` says the file is editable.
The agent reads exactly the object the file's row names, whitespace and all, and an empty file as empty.
The change is saved the way the file workspace saves a file (`filesave.Service.Replace`): a new object over the version the agent read, started again when someone saved meanwhile, the file's chat copies moved with it, and the file re-indexed for search.
Delete, move and rename stay limited to the agent's own files: the file stays the user's.

There is no version history (ADR 0x043).
An agent's edit replaces the file's content exactly as a save by the user does, and the content it replaced is not kept, so an agent's edit is undone by hand.

### 5. Office comments on the server

`internal/officecomments` reads and writes the comments inside Word, PowerPoint and Excel files in Go, one package per format, writing what the browser writes (ADR 0x045 §2) so a comment made on either side opens on the other and in Office.

- Word: comments.xml and its companions; a new thread splits runs at the quote's ends and writes the range markers and reference as the browser does.
- PowerPoint: modern comments, with the moniker list, `ac:context` hash and `a16:creationId` PowerPoint checks for a comment on a range of a shape's text.
- Excel: threaded comments with the persons part, and the legacy note and VML shape Excel needs.

Parts are edited as text where an XML decoder finds them, never marshalled back (which would change namespace prefixes), and untouched zip entries are copied as they are.
Text written into a part drops the characters XML cannot hold, as the browser does.
A file is read within limits: each part (a worksheet, which a note is linked from, only against the total) and all parts together may inflate only so far, and elements may nest only so deep, so a crafted file cannot exhaust memory or the stack; a part that cannot be read or parsed fails the change rather than being taken for a missing one, and the agent is told the file is too large or not readable, whichever it is.
A Word comment from before paragraph ids (Word 2007) cannot be replied to or resolved, and the tool says so rather than write a reply Word shows as a thread of its own.
A change is a new version of the file, saved through `filesave.Service.Replace` over the version read (`filecomments.Office`), with the same no-history rule as §4.

People in files: a WhatIff user or agent is a person with the provider `WhatIff` and, as user id, the user's id or the agent's whole `agent:<id>`; the browser and the server map them back the same way.

## Consequences

- The user and the agent can work on a file together through its comments, in every format: the user asks in a thread, the agent changes the file and answers in the thread, and the chat links to what it did.
- An agent can change the content of a user's file only after the user mentioned it on that file, from that chat.
- An agent's edit, and every comment the agent writes into an Office file, replaces the file's content with no earlier version kept; nothing an agent does to a file can be undone except by hand.
- An Office file the server commented in differs in bytes, not meaning, from the same comment saved by the browser (Word numbers `w:id` from the highest one; PowerPoint edits a slide's comment part in place; Excel writes dates in UTC).

## Later

- **Version history** for every save, by people and agents alike, with restore from the file workspace, is future work across the app; agent edits get it with everything else.
- **Regions of an image** as anchors (ADR 0x045).
- **Telling the user** when the agent mentions them in a comment.

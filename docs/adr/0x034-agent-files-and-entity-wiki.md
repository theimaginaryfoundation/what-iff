# ADR 0x034: Agent files and the entity wiki, as loadable tools

- **Status:** Proposed (experimental)
- **Date:** 2026-10-30
- **Deciders:** What Iff maintainers
- **PR:** _this PR_
- **Supersedes:** the experimental PRs #257 (`read_file` / `grep_files`) and #262 (revisioned `write_file` workspace), which are closed in favour of this restart.
- **Builds on:** loadable built-in tools (`load_tools`, the catalog's `Loadable` flag), gallery folders, and memory pinning (`Personality.auto_pin_memories`).

## Context

Agents can keep two kinds of durable text today: memories (short facts, retrieved by meaning) and the per-personality scratchpad (2KB, rewritten whole).
Neither fits two things agents keep wanting to do:

- **Keep working files**: a journal, a running checklist, a draft, a log of what a recurring job found.
  These are longer than a memory, are read front to back rather than by similarity, and change by appending or editing a few lines.
- **Keep notes about the people, places and projects that come up**, so "Chris" means the same Chris next week.
  That is a lookup by name, not by meaning, and the notes link to each other.

PRs #257 and #262 explored the first one.
They worked, but built a parallel workspace (its own tables, its own `agent/` and `chat/` roots, a revision history) next to the file area users already have, and were always-on tools.
We want to ship something smaller first, use it, and iterate before any of it is wired into the automatic pre- and post-processing (memory extraction, checkpoint summaries).

## Decision

### 1. Two tool families, loadable only

Four tools, grouped by intent so few schemas are loaded:

| Tool | Actions |
|---|---|
| `agent_file_write` | create, append, patch (unified diff), delete, copy, move |
| `agent_file_read` | read (front to back, paged by line), search (the section under a Markdown heading) |
| `entity_write` | write (create), update (replace), append |
| `entity_get` | get up to 5 entities by name |

All four are catalog entries with `Loadable: true`, `AgentDefault: true`, `UserToggleable: true` and `SandboxPolicy: SandboxNever`.
The model sees them by name in the developer context and loads them with `load_tools` when it needs them; they unload at a checkpoint that did not use them.
**They are not called from pre- or post-processing.**
Automatic saving (summarizers writing entities, extraction writing files) is deliberately later work, once we have seen how agents use the tools by hand.

### 2. Agent files are ordinary file attachments

An agent file is a `file_attachments` row like any upload, so it shows in the gallery, in `list` (`kind="files"`), and can be moved or deleted by the user as usual.
Three columns are added: `agent_authored`, `created_by_personality_id`, `created_by_chat_id`, plus `updated_at` for the last content change.
Objects live under `users/{uid}/agent/`, so account deletion already purges them.
They are **never chunked or embedded**: they are read front to back, and `find_context`'s fetch mode reads them straight from the object store instead of from chunks.

Files are addressed by id or by a `folder/name` path (the gallery folder plus the display name).
Creating a file at a path where the user already has a file is refused, so a path names one file in practice.
Text types only (`.md .txt .json .jsonl .yaml .yml .csv .tsv .toml .xml .html .log`), 1 MB per file, 256 KB per call, 1000 agent files per account.

### 3. Agents change only what they created

An agent may change, move or delete a file only when it is agent-authored **and** was created by the conversation's personality (any conversation with that personality), or, when the conversation has no personality, by that conversation.
Everything else is read-only to agents, **including every file the user uploaded**.
To work from a user's file the agent copies it to a new file of its own; the original is never touched.

This is the user's rule: agents must not edit user files.
An approval flow that lets an agent propose an edit to a user file is planned, and will always be opt-in.

### 4. Edits write a new object and swap

Append and patch upload the new content as a new object and then move the row's `s3_key` to it with a conditional update (`WHERE s3_key = <the key that was read>`).
If another writer got there first, the upload is dropped and the edit is retried on the fresh content (three attempts), so two concurrent appends both land.
The replaced object is deleted unless a reference copy (a `find_context` context row) still points at it.
There is no revision history: #262's history was never used, and the version a reference row keeps is enough for now.

### 5. Patches are unified diffs, matched by text

`patch` takes a single-file unified diff.
Hunks are located by their context and removed lines, nearest the line number in the header; line counts are ignored and a header without numbers (`@@ @@`) is accepted, because models get those wrong.
Trailing whitespace is tolerated in matching, and context lines keep the file's own text.
All hunks apply or none do, and a hunk that does not match is refused with a message telling the agent to re-read the file.

### 6. Listing: extend `list`, no new tool

`list` (`kind="files"`) gains `origin` (`agent` / `user`) and `view="tree"`, which draws the page's files as a folder tree like the `tree` command.
Rows of agent files carry `origin: "agent"`.
There is no separate `list_agent_files` tool.

### 7. Entities are a small Postgres table

`agent_entities` holds one row per entity: a normalized key (lowercased, trimmed, inner whitespace collapsed), the display title, and a body of at most 5,000 characters.
Lookups are exact on the normalized key; fuzzier matching ("Chris S" finding "Chris Smith") is later work.
A read returns the whole body, and lists the `[[Name]]` links in it so the agent can follow the graph.
Tool descriptions ask for wiki-style pages: durable facts, one subject per page, other nouns linked as `[[Name]]`.

Writes over the cap are refused with a message to condense the page or move detail to a linked entity, never truncated.
Updates are a compare-and-swap on the body, retried, so concurrent appends both land.

### 8. Entity scope follows memory pinning

Scope works exactly like user memories:

- A new entity written by a personality with `auto_pin_memories` on is pinned to that personality, and only it sees it.
- Otherwise the entity is global, and every personality (and conversations with none) sees it.
- A read sees global entities plus the active personality's pinned ones. When both exist for a key, the pinned one wins.
- `write` is refused when the key is already visible (global or pinned), so a pinned personality never silently shadows a shared page with a new one; it updates the shared page instead.

Two partial unique indexes enforce one global entity per key and one pinned entity per (personality, key).
Deleting a personality deletes its pinned entities; global ones stay.

### 9. The user sees entities in the Memories panel

Memories gets an **Entities** tab between Thread and Summaries.
It lists entities (search, filter by personality or global), renders `[[links]]` as clickable searches, and lets the user edit an entity's text or delete it.
The API is `GET /memory/entities`, `PATCH /memory/entities/{id}` and `DELETE /memory/entities/{id}`.
A user edit clears the "last written by" provenance.
Creating entities from the UI is not in this change.

### 10. Containers stay private

The private build adds `load_file_to_container`, which copies a file into the conversation's c4a container, and guidance that points agents at the container for anything beyond these basics (running code, transforming data, large edits).
The public tools stay basic on purpose.

## Alternatives considered

- **A separate workspace with its own tables (#262).** More control (revisions, per-conversation roots) but a second file system next to the one users see, and none of it visible in the gallery. Rejected for v1.
- **One tool per verb** (`create_file`, `append_to_file`, … `get_entity`): about 13 loadable tools. Each loaded tool costs its schema on every request, and loading them one by one costs turns. Rejected for the four grouped tools.
- **A `list_agent_files` tool.** Duplicates `list`. Rejected for an `origin` filter and a tree view on `list`.
- **Edits as exact snippet replacement** (#262's `edit` mode). Simpler to apply, but the user asked for diff semantics, and diffs carry several changes in one call. Patch matching by text keeps most of the robustness of snippets.
- **Overwriting the object in place.** Simpler, but a reader could see a partial write, concurrent appends could lose data, and context reference rows would change under the reply that showed them.
- **Entities as memories with a key.** Memories are retrieved by meaning and merged automatically; entities need exact lookup by name and must not be merged behind the agent's back.
- **Entity scope per personality always, or global always.** Following the existing memory toggle keeps one mental model for users who already chose how isolated each personality is.

## Consequences

- Agents can keep real working files and a linked set of notes, visible to and editable by the user.
- User files cannot be changed by an agent at all, which is safe but means an agent "editing" a user document works on a copy.
- Agent files whose personality is deleted stay in the gallery but can no longer be changed by any agent.
- Every append rewrites the whole file (at most 1 MB), which is fine at this size and would need segments for large logs.
- Entity lookup is exact; near-duplicate entities ("Chris" and "Chris Smith") are possible until fuzzy matching lands. Descriptions ask the agent to check first.
- `file_attachments` and the new `agent_entities` table are created by auto-migration; there is no data backfill.

## Not done yet

- Opt-in approval flow for agents proposing edits to user files.
- Wiring entities and files into pre/post-processing (summarizers saving entities, retrieval pulling in linked entities).
- Fuzzy entity matching and entity aliases.
- Creating entities from the UI, and showing an entity's links back ("what links here").
- Revision history or undo for agent files.
- Including entities in account export and import.

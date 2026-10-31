# ADR 0x036: Agents archive wrong memories; the user can restore them

- **Status:** Proposed
- **Date:** 2026-10-31
- **Deciders:** What Iff maintainers
- **Issue:** none; raised by the Vix persona while testing the entity wiki in PR #406
- **PR:** [#416](https://github.com/theimaginaryfoundation/what-iff/pull/416)
- **Builds on:** the entity wiki's agent delete in [ADR 0x034](0x034-agent-files-and-entity-wiki.md) (PR #406, which archives in the same way), the memory merge audit log (`memory_merge_events`) and its undo, and memory provenance from the Discord relay ([ADR 0x025](0x025-discord-relay.md)).

## Context

Agents write memories (`create_memory`, and checkpoint extraction) but cannot take one back.
A memory that was wrong when written, or became wrong later ("Lives in Berlin" after the user moved), stays in recall until the user finds it in the Memories panel.
The agent keeps being told the stale fact on every turn where it is relevant, and the only correction it can make is to add a contradicting memory next to it.

The user's direction was that an agent's "delete" must be an archive, not a delete.
It should work like merges do today: the memory is archived and drops out of search and `find_context`, but nothing is deleted.
If an agent archives something important by mistake, the user can get it back.

Memories already have that mechanism.
`memories.status` is `active` or `inactive`.
A merge (fold) sets the memories it absorbs `inactive`, and the user archives a memory the same way: the Memories panel's Archived view lists inactive memories and Unarchive restores them.
Every read path that feeds the model or the user filters on `status = active`: both vector searches (`GetRelatedMemories` for prefetch and `find_context` search, investigate and related; `GetRelatedSummaryMemories`), the default Memories list, the short-prefix lookup, merge candidates, export and embedding backfill.
Every merge and link is logged in `memory_merge_events`, which the Memories panel shows as *Merge history* with an Undo button, and which agents read with `find_context` `lifecycle_events`.

## Decision

### 1. A new loadable tool, `archive_memory`

Agents archive a memory with a new tool, `archive_memory`, rather than an action on an existing one.
ADR 0x034 groups tools by intent (`entity_write` holds write, update, append and delete), but the memory write tool is `create_memory`, which is always offered, named for creating, referenced by users' `disabled_tools` and by the relay code (`CreateMemoryToolWithOrigin`).
Renaming it to a grouped `memory_write` would break those references for one new action.

`archive_memory` is `Loadable` (listed by name until the model loads it, like the entity tools), on by default and user-toggleable.
Archiving is rare, so its schema should not cost tokens on every request.
Its description and result say "archive" and tell the agent that nothing is deleted and that the user can restore the memory from the Archived view or Merge history, so the agent archives a wrong memory without hesitating and can tell the user how to get it back.

Arguments: `memory_id` (required) and an optional short `reason` (at most 500 characters).

### 2. Exact id only

`memory_id` must be one full memory UUID, bare or as `memory:<uuid>`, which is the form `find_context` prints for every memory.
A short id prefix, which `find_context` `related`/`origin` accept, is refused, as is any description.
There is no lookup by content or similarity: archiving never chooses between memories.
An unknown id is "not found".

### 3. Only what this conversation recalls

A conversation may archive only memories its own recall reads (the same rule as `relatedMemoryReadable`): its own thread memories, and User-scoped memories that are unpinned or pinned to its personality.
Another conversation's thread memory, or one pinned to another personality, is refused with a pointer to the Memories panel.

The memory's own state is checked atomically in the datastore, under a row lock:

- a starred memory is refused (the user has marked it as one they value, as the merge rewrite already respects);
- a checkpoint summary is refused (checkpoints rewrite it);
- a memory already archived (by an agent, by the user, or by a merge) is refused, saying which.

In a Discord relay thread (an external-provenance turn), people outside the account are talking to the agent, so only an external memory may be archived there, never one of the owner's own.
A sandboxed conversation is not offered the tool, and the handler refuses there too.

### 4. Archive exactly as a merge does, and record who did it

An agent's archive sets the memory `inactive`, as a merge sets the members it absorbs.
The row, its content and its embedding are kept; nothing is deleted.
Every active-only read path already excludes it, so no query changes and nothing can miss the new rule.
The kept embedding means a restore needs no re-embedding.

Three nullable columns on `memories` say that an agent did it, and who: `archived_at`, `archived_by_personality_id` and `archived_by_chat_id`.
The ids are plain columns, not foreign keys, so the record survives deleting the personality or conversation.
The columns are cleared when the memory is restored; the event (below) keeps them for good.

A fetch by id (`find_context` `fetch memory:<uuid>`) can still reach an archived memory.
The shared memory formatter marks any inactive memory `archived(…)`, with the time when an agent archived it, so the model knows it is not in active memory.

### 5. Archived memories do not come back through the carry-over

Memory lines loaded on an earlier turn are persisted with that turn and replayed into the next few (the carry-over).
A sandboxed chat already re-checked them against its own memories; an ordinary chat replayed them as they were, so a memory archived since (by an agent, a merge or the user) kept appearing for a few turns.
The replay now drops a memory line whose id is no longer active (`ActiveMemoryIDs`, one query per turn and only when there are persisted memory ids), and the sandbox re-check also requires the memory to be active.
Lines without an id are kept as before, and a failed lookup in an ordinary chat keeps the replay as it was rather than dropping context.

### 6. An `archive` lifecycle event the user can undo

Each archive writes a `memory_merge_events` row with the new `merge_type` `archive`: the archived memory as `survivor_memory_id`, its content, a one-member `source_members` snapshot, and the archiving personality, conversation and reason in the snapshot (`archival` in the API).
The event appears in Memories > Merge history ("Archived by an agent", with the reason) and in `find_context` `lifecycle_events` (with `reason`).
Undo on the event restores the memory and clears its archive columns.
If the user has deleted the memory since, undo answers 404.

The memory is also in the Archived view, and Unarchive there restores it the same way the user restores any inactive memory (PATCH status to active).
That clears the archive columns and closes the open `archive` event (sets `reverted_at`), so an old event cannot later be undone on top of a newer archive.
The memory's page says that an agent archived it, and when.

`merge_type` is an ent enum, stored as a varchar in Postgres, so the new value and the three nullable columns go out through the normal auto-migration with no data change.

### 7. No hard delete from the agent; the user's own delete is unchanged

No agent path deletes a memory row.
Deleting a memory in the Memories panel still removes it outright: it is the user's own correction, as in ADR 0x034.

## Alternatives considered

- **A hard delete by the agent.**
  Rejected by the user: an agent's mistake would lose the memory for good.
- **A separate tombstone (`deleted_at`).**
  Memories are read from about twenty places that already filter on `status`; a second visibility flag would have to be added to each of them, and one missed is an archived memory coming back into recall.
  It would also put the agent's archives somewhere other than the Archived view the user already uses.
- **A third status value.**
  It would carry the same information as `archived_at`, but every place that treats status as active/inactive (the frontend type, the Archived view, `normalizeMemoryStatus`, fold undo's prior-status bookkeeping) would need to learn it.
- **An action on `create_memory`, or renaming it to a grouped `memory_write`.**
  See decision 1.
- **Prefix or content matching, refusing only when ambiguous.**
  `find_context` prints full ids, so the agent always has one, and a prefix that happens to be unique today is still a guess about which memory was meant.
- **Archiving many memories per call.**
  One id per call keeps every archive a single, reviewable event.
- **Allowing it in a sandbox, limited to the sandbox's own memories.**
  Possible, but a sandbox is where strangers talk to the agent, and nothing there needs it yet.

## Consequences

- Agents can correct memory: archive the stale memory, then `create_memory` the corrected fact.
- Each agent archive is visible to the user with its reason, in the Archived view and Merge history, and can be restored from either.
- Merge history is no longer only merges; its intro, tab tooltip and the API's `merge_type` description say so.
- Archived memories stay in the database until the user deletes them; they count toward nothing the model sees unless it fetches one by id.
- A memory pinned to another personality, or in another conversation, needs the user (or that personality) to archive it.
- Ordinary chats make one more small query per turn when earlier turns carry memory lines.

## Not done yet

- Telling an agent's archives apart from the user's and from merged memories in the Archived list (the API has `archived_at`; the memory's page already says so).
- Showing the archiving personality and conversation by name in Merge history (the ids are in the API).
- Reviewing archived entities (ADR 0x034) and archived memories from one place.

# ADR 0x036: Agents remove wrong memories with an auditable soft delete

- **Status:** Proposed
- **Date:** 2026-10-31
- **Deciders:** What Iff maintainers
- **Issue:** none; raised by the Vix persona while testing the entity wiki in PR #406
- **PR:** [#416](https://github.com/theimaginaryfoundation/what-iff/pull/416)
- **Builds on:** the entity wiki's agent delete in [ADR 0x034](0x034-agent-files-and-entity-wiki.md) (PR #406), the memory merge audit log (`memory_merge_events`) and its undo, and memory provenance from the Discord relay ([ADR 0x025](0x025-discord-relay.md)).

## Context

Agents write memories (`create_memory`, and checkpoint extraction) but cannot take one back.
A memory that was wrong when written, or became wrong later ("Lives in Berlin" after the user moved), stays in recall until the user finds it in the Memories panel and deletes it.
The agent keeps being told the stale fact on every turn where it is relevant, and the only correction it can make is to add a contradicting memory next to it.

PR #406 gave the entity wiki an agent delete with three rules: an exact target only, a refusal rather than a guess when the target is unclear, and a soft delete that keeps the row and records who removed it and when.
This ADR gives memories the same correction semantics.

Memories already have a soft-unlisting mechanism.
`memories.status` is `active` or `inactive`: a merge (fold) sets the memories it absorbs `inactive`, and the user archives a memory the same way (the Memories panel's Archived view lists inactive memories and restores them).
Every read path that feeds the model or the user filters on `status = active`: vector recall (`GetRelatedMemories`, `GetRelatedSummaryMemories`), the Memories list, the short-prefix lookup, merge candidates, export and embedding backfill.
Every merge and link is logged in `memory_merge_events`, which the Memories panel shows as *Merge history* with an Undo button, and which agents read with `find_context` `lifecycle_events`.

## Decision

### 1. A new loadable tool, `remove_memory`

Agents remove a memory with a new tool, `remove_memory`, rather than an action on an existing one.
ADR 0x034 groups tools by intent (`entity_write` holds write, update, append and delete), but the memory write tool is `create_memory`, which is always offered, named for creating, referenced by users' `disabled_tools` and by the relay code (`CreateMemoryToolWithOrigin`).
Renaming it to a grouped `memory_write` would break those references for one new action.

`remove_memory` is `Loadable` (listed by name until the model loads it, like the entity tools), default on and user-toggleable.
Removal is rare, so its schema should not cost tokens on every request.

Arguments: `memory_id` (required) and an optional short `reason` (at most 500 characters).

### 2. Exact id only

`memory_id` must be one full memory UUID, bare or as `memory:<uuid>`, which is the form `find_context` prints for every memory.
A short id prefix, which `find_context` `related`/`origin` accept, is refused, as is any description.
There is no lookup by content or similarity: removal never chooses between memories.
An unknown id is "not found".

### 3. Only what this conversation recalls

A conversation may remove only memories its own recall reads (the same rule as `relatedMemoryReadable`): its own thread memories, and User-scoped memories that are unpinned or pinned to its personality.
Another conversation's thread memory, or one pinned to another personality, is refused with a pointer to the Memories panel.

The memory's own state is checked atomically in the datastore, under a row lock:

- a starred memory is refused (the user has marked it as one they value, as the merge rewrite already respects);
- a checkpoint summary is refused (checkpoints rewrite it);
- a memory already removed, or otherwise inactive (archived by the user or folded away by a merge), is refused, saying which.

In a Discord relay thread (an external-provenance turn), people outside the account are talking to the agent, so only an external memory may be removed there, never one of the owner's own.
A sandboxed conversation is not offered the tool, and the handler refuses there too.

### 4. Reuse `inactive`, with removal provenance on the row

A removal sets the memory `inactive`, the same status a merge uses.
Every active-only read path already excludes it, so no query changes and nothing can miss the new rule.
The embedding is kept, as merges keep theirs (#250), so a restore needs no re-embedding.

Three nullable columns on `memories` tell a removal apart from a merge and say who did it: `removed_at`, `removed_by_personality_id` and `removed_by_chat_id`.
The ids are plain columns, not foreign keys, so the record survives deleting the personality or conversation.
The columns are cleared when the memory is restored; the event (below) keeps them for good.

A fetch by id (`find_context` `fetch memory:<uuid>`) can still reach a removed memory, and the shared memory formatter marks it `removed=<time>(no longer in active memory)`.

### 5. A `remove` lifecycle event, undone like a merge

Each removal writes a `memory_merge_events` row with the new `merge_type` `remove`: the removed memory as `survivor_memory_id`, its content, a one-member `source_members` snapshot, and the removing personality, conversation and reason in the snapshot (`removal` in the API).
The event appears in Memories > Merge history ("Removed by an agent", with the reason) and in `find_context` `lifecycle_events` (with `reason`).
Undo on the event sets the memory active again and clears its removal columns.
If the user has deleted the memory since, undo answers 404.

A removed memory is also listed in the Memories panel's Archived view, and the user can restore it there, which sets its status to active (PATCH).
That clears the removal columns and closes the open `remove` event (sets `reverted_at`), so an old event cannot later be undone on top of a newer removal.

`merge_type` is an ent enum, stored as a varchar in Postgres, so the new value and the three nullable columns go out through the normal auto-migration with no data change.

### 6. The user's own delete is unchanged

Deleting a memory in the Memories panel still removes the row outright: it is the user's own correction, as in ADR 0x034.

## Alternatives considered

- **A separate tombstone (`deleted_at`) like the entity table.**
  The entity table was new, so its every query could be written with the tombstone in mind.
  Memories are read from about twenty places that already filter on `status`; a second visibility flag would have to be added to each of them, and one missed is a removed memory coming back into recall.
- **A third status value (`removed`).**
  It would carry the same information as `removed_at`, but every place that treats status as active/inactive (the frontend type, the user's status filter, `normalizeMemoryStatus`, fold undo's prior-status bookkeeping) would need to learn it.
- **An action on `create_memory`, or renaming it to a grouped `memory_write`.**
  See decision 1.
- **Prefix or content matching, refusing only when ambiguous.**
  `find_context` prints full ids, so the agent always has one, and a prefix that happens to be unique today is still a guess about which memory was meant.
- **Hard delete by agents.**
  It cannot be audited or undone, and a wrong removal by a confused or prompt-injected agent would lose the user's data.
- **Removing many memories per call.**
  One id per call keeps every removal a single, reviewable event.
- **Allowing removal in a sandbox, limited to the sandbox's own memories.**
  Possible, but a sandbox is where strangers talk to the agent, and nothing there needs it yet.

## Consequences

- Agents can correct memory: remove the stale memory, then `create_memory` the corrected fact.
- Each removal is visible to the user with its reason and can be undone from the panel they already use for merges.
- Merge history is no longer only merges; its intro, tab tooltip and the API's `merge_type` description say so.
- Removed memories stay in the database until the user deletes them; they count toward nothing the model sees.
- A memory pinned to another personality, or in another conversation, needs the user (or that personality) to remove it.

## Not done yet

- Telling removed memories apart in the Archived list (they are listed with archived and merged ones); the memory's own page already says an agent removed it, and when.
- Showing the removing personality and conversation by name in Merge history (the ids are in the API).
- Removal of entities and memories from one place in the UI (the entity tombstones from ADR 0x034 have no review screen yet).

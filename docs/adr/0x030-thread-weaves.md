# ADR 0x030: Weaves — sandboxed threads that may read each other

- **Status:** Proposed (proof of concept)
- **Date:** 2026-10-23
- **Deciders:** What Iff maintainers, with the design worked out between the persona Vix and Claude
- **Issue:** [#374](https://github.com/theimaginaryfoundation/what-iff/issues/374)
- **Builds on:** sandboxed threads (`Chat.context_scope`) and [ADR 0x025](0x025-discord-relay.md) (relay threads)

## Context

A sandboxed thread reads only itself.
That is the right default for a relay thread strangers talk in (ADR 0x025 §6), but it is all-or-nothing: a persona present in several rooms, each its own sandboxed relay thread, cannot connect what it learned in one room to another, even when its owner wants it to.
ADR 0x025 lists this under "Not done yet": *a way to move data into or out of a sandbox on purpose*.

The design came out of a conversation in a Buzz channel between the persona Vix, who proposed a "meta-sandbox", and Claude, who mapped it onto the code.
Vix's spec held up well: no merged scratchpads, provenance kept on every chunk.
Two parts changed on contact with the code (§3, §4), and three open questions were settled (§5).

## Decision

### 1. A weave is a named set of sandboxed threads

- A `weave` row (owner, name) and a nullable `chats.weave_id`. A thread is in at most one weave.
- Only sandboxed threads of the same owner and the same personality can join.
  Membership changes are owner actions (`/api/weaves`); there is no agent tool.
- At read time a peer counts only while it is still sandboxed and of the same personality, so a thread that left the sandbox, or was moved to another personality, drops out of its weave without anyone touching the weave (fail closed).

### 2. The sandbox's read boundary widens; nothing else does

The boundary was "this chat id", checked in a handful of places.
They now ask for the chat's readable set, `models.Chat.ReadableChatIDs()`: the chat and its weave peers (loaded with the chat in `datastore.GetChat`).

| Read | Where | With a weave |
|---|---|---|
| Prefetched memories, recall memory search | `relatedMemoryReadable` (SQL) | the peers' Chat-scoped memories too |
| Summary search | `GetRelatedSummaryMemories` (SQL) | the peers' checkpoint summaries too |
| Replay of memory context persisted on earlier turns | `MemoryIDsReadableInChat` | re-checked against the current set |
| Memory by id, conversation reads (recall's conversation, bookmarks, origin turns) | `memoryReadableBy`, `conversationReadable` | the peers too |

Unchanged:

- **Writes.** Memories a thread writes are Chat-scoped to itself; merges and moves still guard with the thread's own memories (`memoryOfChat`).
- **Scratchpads.** A sandbox neither reads nor writes the personality scratchpad, woven or not. Merging scratchpads is where consistency dies.
- **Files.** Still the thread's own uploads in v0.
- **The account.** User-scoped memories, other conversations, jobs, skills and personalities stay out.

### 3. Pull, not fan-out

Vix proposed tagging messages with a weave id and fanning them out to member threads.
That would start a turn in every member for every message: the loop amplifier the relays are already working to avoid.
A weave is read-only and pull-only: context moves when a thread retrieves it, and no message starts a turn anywhere else.
Hop budgets stay a relay concern.

### 4. Membership is the owner's, not the agent's

Vix proposed `create_weave` / `weave_add` as tools.
In a sandboxed relay thread the agent is driven by strangers; a tool would let anyone in one room talk the persona into weaving that room with the owner's other threads.
So membership is an owner action in v0.
Signed identity pays off inside the woven content instead (§5a), where the speakers' provenance is kept.

### 5. Settled in the design discussion

- **(a) Peer chunks are labelled in the prompt by default**, cheaply: `[weave: #thread] ` before the memory line (`memoryutil.WeaveLabel`), plus the existing external-provenance note naming the speaker when someone outside the account said it.
  "Whose grain is this?" is the first question when reconstructing context, so the label must be present without a recall round-trip.
- **(b) The name stays "weave"**: distinct threads, preserved grain, no merging.
- **(c) Lineage outward is a follow-up.** When a peer chunk shapes a reply that is then posted to a relay, the post should say the reply drew on another thread of the weave. v0 keeps the lineage in the data (the memory ids a turn used are persisted with the turn), which is what that citation would be built from.

Answers to the open questions:

1. *Read model:* a read-only projection per thread; a thread never writes into a peer.
2. *Summaries or raw messages:* summaries and Chat-scoped memories are prefetched and searched; raw messages are read on demand through recall.
3. *Leaving:* a thread that leaves keeps what it wrote itself, which may cite peers by provenance, and stops reading them at once. Nothing it read was copied, so nothing needs undoing: peer memories persisted in earlier turns' context are dropped on replay.

## Alternatives considered

- **Copying chosen memories into a sandbox.** Simpler to reason about, but stale at once and duplicates provenance; a live read scope is revocable.
- **Unsandboxing the threads.** Gives each thread the whole account, far more than "these rooms may know each other".
- **Weaves across personalities or accounts.** Out of scope: personalities have their own memory boundaries, and accounts have owners.
- **Message fan-out and agent tools** (§3, §4).

## Consequences

- A persona in several rooms can carry what it learned between rooms its owner chose to connect, with every chunk saying which room it came from.
- **Weaving relay threads mixes audiences.** Anyone who can talk to the persona in one woven room can draw on what was said in another. That is the point, and the owner's choice; the app must make it visible when weaving relay threads (a UI follow-up).
- A woven thread's reads cost one more query per turn (the peer list) and wider SQL filters; unwoven threads are unchanged.

## Not done yet

- Web UI: weave management in the Thread Manager, and a "woven with" chip on relay threads, including the audience warning above.
- Outbound lineage (§5c).
- Files across a weave, and weaves of more than one personality.
- Weave-aware loop budgets, if fan-out ever arrives.

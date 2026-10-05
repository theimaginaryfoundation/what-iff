# internal/exporter

Clean, id-stripped **projection** of a user account into an export ZIP tree (ADR 0x018 MVP). It
deliberately drops raw Postgres identity so an export loads into any account without primary-key
collisions — unlike the admin ZIP backup (`internal/datastore/accountbackup.go`), which preserves
source UUIDs for clone-style restore.

## What it produces (into a `Tree` sink)

- `conversations.json` — Anthropic (Claude) shape, so exports round-trip back through our own importer
  (`internal/handlers/chat/anthropic_import.go`). `chat.ID` is emitted only as the dedup `uuid`, never
  persisted as a DB key on import. Optional `whatiff_*` fields preserve checkpoint/window, personality
  association, and continuation UI state without affecting standard Anthropic consumers. `ParseConversations`
  reverses this for the account-import path.
  `whatiff_memory_sensitivity_limit` carries a restricted chat's memory sensitivity limit; the unrestricted default is not written, and an export without it imports as unrestricted.
- `personalities/{id}/personality.json` — source personality ID, name, system prompt, scratchpad, auto-pin
  (self-contained). Source IDs are relationship references and are remapped on import.
  An imported SillyTavern card's passthrough blob rides along as `character_card` so account import keeps it.
- `personalities/{id}/sillytavern.json` — the personality as a `chara_card_v2` document (`WriteSillyTavernCards`, built by `internal/stcard` with the account's username as `creator`).
  It is a courtesy export for other tools; account import reads `personality.json`, not this file.
- `files/manifest.json` — inventory of the user's S3 objects (keys/sizes/etags); no bytes.
- `manifest.json` — schema version, export time, account, per-section counts.

(Memories are not projected here; the export embeds them as a nested `memories.zip` in the existing
memory-export format so `ds.ImportMemories` consumes them directly.)

## Shape

- `Tree` is the sink: `ZipTree` (production, over `archive/zip`) or `MemTree` (tests). The projection
  is pure and unit-tested; conversation output is deterministic (canonical ordering).
- Orchestration (fetch → project → ZIP → S3 → presign → email; and import) lives in
  `internal/handlers/accountexport`; the datastore fetchers are in `internal/datastore/accountexport.go`.

The richer git-bundle projection (per-checkpoint history replay, `gitbundle` subpackage) is deferred
to branch `export-account-followup`.

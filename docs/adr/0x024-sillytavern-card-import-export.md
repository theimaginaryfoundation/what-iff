# ADR 0x024: SillyTavern character-card import and export for personalities

- **Status:** Proposed
- **Date:** 2026-10-01
- **Deciders:** What Iff maintainers

## Context

Users asked to bring SillyTavern characters in and out of What Iff.
A SillyTavern card (`chara_card_v2`) models a *prompt-manager* character: separate description, personality and scenario fields, a first message and alternates, example dialogue, a post-history instruction injected after the chat, and a character book with keyword-triggered lore.
A What Iff personality is a single system prompt plus a scratchpad, with no greeting slot and no runtime lore injection.
We want portability without pretending to run the parts of the spec we do not implement.

## Decision

**Flatten on import, reconstruct on export, preserve everything we do not understand.**

1. **Import** flattens `system_prompt`, `description`, `personality` and `scenario` into the one system prompt under exact-match `### [Description]` / `### [Personality]` / `### [Scenario]` headers (the bare `system_prompt` first, no headers when it is the only field).
   The character book becomes a `### [Lore]` section when it has 5 or fewer enabled entries and one file attachment per entry otherwise.
2. **Behavior we do not model is not applied.**
   Greetings and `mes_example` have no slot.
   `post_history_instructions` is deliberately not appended to the prompt: injecting instructions after the chat history changes model behavior in a way the card author wrote for a different runtime.
3. **The whole `data` object is stored verbatim** as an opaque passthrough blob, so export can return fields we never understood.
4. **Export** starts from the blob (when there is one), then lays the personality's current name and prompt over it, splitting the prompt back into card fields by its headers.
   If the headers are absent or were edited, the whole prompt exports as `system_prompt`, and exports that relied on the headers say so in `creator_notes`.
5. **PNG cards** (the format cards are mostly shared in) use the personality's cover image as the picture.
   Import reads the card out of the PNG's `chara`/`ccv3` text chunk; the web client then attaches the same picture as the cover image, the same client-orchestrated way it uploads cover photos and lore files.
   Export with `?format=png` embeds the card in the cover image (re-encoded as PNG if it is not one already) and is only offered for personalities that have a cover.
6. **Length limit: shed, then refuse.**
   If the flattened prompt exceeds the 25,000-character limit, the ancillary `scenario` and then `personality` sections are left out of the prompt and the user is warned.
   They stay in the stored card, and the personality records which fields were omitted so export restores them (until the user writes their own section for them).
   If the prompt is still too long, the import is refused with `system_prompt_too_long` and the web UI explains the limit and what to shorten.
7. Surfaces: `POST /personality/import/sillytavern`, `GET /personality/{id}/export/sillytavern`, an Import card button on the personalities list, Export card / Export PNG card buttons on the personality page, and one `sillytavern.json` per personality in the account export (the card data also rides in `personality.json`, so an account export/import keeps it).

### Where the blob lives: its own table

The blob is a `PersonalityCard` row (1:1, cascade delete), not a column on `personalities`.
Personalities are loaded on hot paths — `GetPersonality` on every message send, `ListPersonalities` for every list page — and a card can carry megabytes of lore the agent never reads.
A separate table means only a card export or an account export ever loads it.
`models.Personality.CharacterCard` is a create-time input only (`json:"-"`): it never reaches API responses and is never filled on read.

### Judgement calls beyond the original brief

- **`creator`** is the exporting user's WhatIff username only when the original card named no author.
  The brief says "`creator` = the WhatIff username of the owner" *and* "keep creator intact from the blob"; these collide for an imported card, and overwriting would credit the importer with someone else's work.
- **`tags` and `character_version`** come from the blob; personalities have no such metadata of their own yet.
- **The header warning** in `creator_notes` is added only when the export used the headers, not on every file.
- **Lore files are uploaded by the client** after the server creates the personality.
  The attachment pipeline (provider upload, S3, chunking) is bound to a multipart HTTP request; the server returns the entries (`lore_files`) and the web client uploads each through the existing `POST /personality/{id}/file-attachment`.
  API-only callers must do the same.

## Consequences

- Cards round-trip losslessly for everything outside the prompt, and the prompt round-trips as long as the headers survive.
- Editing a lore file after import does not change the exported `character_book` (the blob's copy is exported).
- The admin account-backup restore does not carry the blob.
- Not handled: v1 cards, `iTXt`/`zTXt` PNG text chunks, `{{char}}`/`{{user}}` macros, and any UI for `first_mes` and the alternate greetings.

# ADR 0x035: Skills as Agent Skills bundles, discovered and loaded by the agent

- **Status:** Accepted 2026-10-31 (design only; nothing implemented yet)
- **Date:** 2026-10-30
- **Deciders:** What Iff maintainers
- **Builds on:** loadable built-in tools (`load_tools`, the catalog's `Loadable` flag, `chat_mcp_tool_states`, checkpoint unload), skill-linked connectors (#290), the agent file tools and gallery-as-VFS work in [ADR 0x034](0x034-agent-files-and-entity-wiki.md) (#406) and its private half (what-iff-private#225, `load_file_to_container`), the containers4agents `shell_exec` tool (private), and Nexus projections (private).
- **Standard:** [Agent Skills](https://agentskills.io/specification), as adopted by Claude Code, the Claude API, Codex, Cursor, Gemini CLI, Copilot, Amp and others.

## Context

A What Iff skill (`Ritual` in code and DB; "skill" everywhere users look) is today a name, a description, a prompt body (`content`), hotkeys, an optional personality binding and linked MCP connectors.
It reaches the model in three ways, each injecting the **whole body** and never the description:

| Path | Where the body lands | Code |
|---|---|---|
| `/skill`, hotkey, API `rituals` on a message | Appended raw to the user message text, with no name or delimiter | `EnrichUserMessageWithRituals`, `FormatRituals` (`internal/agent/rituals.go`) |
| The active mode's attached skills | A developer segment, `- name: body` bullets under "Attached skills for this mode" | `formatMoodRitualsForContext` (`internal/agent/mood_context.go`) |
| A scheduled job's skills | The job prompt, via the first path | `handleEphemeralPrompt` (`internal/agent/agentjob_run.go`) |

The model can list skills (`list(kind="skills")` returns id, name, description) and route them to sub-agents and jobs, but **it cannot read or activate a skill itself**.
There is no index of available skills in its context, so nothing prompts it to look.
Every attached skill costs its full body on every turn, whether or not it is relevant.
Skills carry no files: no reference documents, no scripts, no templates.

The Agent Skills standard describes exactly the shape we want: a `SKILL.md` with `name` and `description` frontmatter and a Markdown body, optional `scripts/`, `references/` and `assets/`, and **progressive disclosure** (metadata always in context, the body when activated, resources only when read).
Every major harness now implements it, and the ecosystem (skills.sh, anthropics/skills, Nexus for us) distributes skills in that shape.
We already have the pieces a skills runtime needs: load-gated tools that persist per chat and unload at checkpoints, per-turn connector scoping by active skills, a gallery becoming a virtual file system agents can read (#406), copying files into a per-conversation container (#225) and running code there (`shell_exec`).

Two other findings from the review shape the decision:

- **Mode and job attachments are one-to-many, not many-to-many.** `Mood.rituals` and `AgentJob.rituals` are `edge.To` without an inverse, so ent made them FK columns on `rituals` (`mood_rituals`, `agent_job_rituals`). Attaching a skill to a second mode or job silently detaches it from the first. Skills meant to be reused need real join tables.
- **Skill bodies are uncapped and unsanitized.** The 25k-char message limit is checked before enrichment; nothing strips invisible Unicode. Both matter once skills arrive from a marketplace.

## Decision

### 1. A skill is an Agent Skills bundle; the database stays the source of truth

The `Ritual` row remains the record of a skill.
`SKILL.md` is its **serialization**: parsed on import, rendered on export and when mounted into a container, never a second copy to keep in sync.
The row gains the spec's fields and two invocation flags:

| Column | Spec field | Rules |
|---|---|---|
| `slug` (new) | `name` | 1–64 chars, `[a-z0-9-]`, no leading/trailing/double hyphen, **unique per owner**. Derived from the display name on create and when importing a skill with no slug; editable. |
| `name` (existing) | `metadata.display_name` | The display title users see; unchanged. |
| `description` (existing) | `description` | Now capped at 1,024 chars and required to say *when* to use the skill, since it is what the model reads to decide. |
| `content` (existing) | body | The Markdown body. Soft limit 5,000 tokens (the UI warns), hard limit 64 KB. |
| `license`, `compatibility` (new) | same | Free text, 500 chars. `compatibility` is shown to the user and to the model when scripts cannot run in this deployment. |
| `metadata` (new, JSON) | `metadata` | String→string map for client extensions. Our own keys are prefixed `whatiff-`. |
| `allowed_tools` (new, []string) | `allowed-tools` | See §6. |
| `disable_model_invocation` (new, bool) | Claude Code / Cursor / Codex `allow_implicit_invocation` | When set, the skill is **not** in the model's index and can only be activated by the user (`/skill`, hotkey, mode, job). |
| `user_invocable` (new, bool, default true) | Claude Code | When false, the skill is hidden from the `/skill` picker and hotkeys; the model may still load it. |
| `hotkeys`, `personality` edge, `mcp_servers` edge | — | Unchanged. Connectors travel in `metadata.whatiff-connectors` on export only as names, since they are account-local. |

Files belong to the bundle (§3).
A skill with no files is still a complete skill; most will stay that way.

### 2. The agent discovers and loads skills itself

This is the mechanism the loadable tools already use, applied to skills.

**Index (tier 1).**
Every turn, the developer context gets a "Skills" block listing the skills in scope for this chat: the owner's skills bound to this personality or to none, minus `disable_model_invocation`, minus skills already active.
One line each, `- slug: description`, sorted by slug so the block is stable for prompt caching.
It is budgeted at **1% of the model's context window** through the context-budget seam (ADR 0x024).
Over budget, descriptions are shortened and then dropped from the least recently used skills first; **slugs are never dropped**, and the block ends with "use `list(kind="skills", filter=...)` to search".
With zero skills in scope the block and the `load_skill` tool are both omitted.
Sandboxed chats have zero skills in scope, as today.

**Load (tier 2).**
A new always-on tool `load_skill` takes `skills: [slug...]`.
For each skill it:

1. Records the skill as loaded for this chat (§4), so the body is injected from the next loop round and on later turns.
2. Returns the body wrapped as `<skill name="slug">…</skill>`, followed by a `<skill_resources>` listing of the bundle's files (path and size, never contents) and the names of its connectors and tools now in scope.
3. Brings the skill's connectors into MCP scope and returns their **tool inventory**: for each connector its id, name, status and the names and one-line descriptions of its tools, with the ones already loaded marked. This is the `list(kind="mcp_servers")` shape, filtered to the skill's connectors, and it costs no extra round trip because the turn's adapter has already discovered those tools (`prepareTurnMCPToolSpecs`). Nothing is loaded from a connector by `load_skill`; the model calls `load_tools` for the tools the task needs, so a skill with a wide connector does not drop every tool definition into the request. A follow-up lets `allowed_tools` name connector tools for the cases where one call should make the skill usable (§6).
4. Loads the tools named in `allowed_tools` that are loadable built-ins here (§6).
5. When the bundle has files, marks `agent_file_read` loaded (ADR 0x034) so references can be read without a second `load_tools` round trip.

`unload_skill` reverses it.
`list(kind="skills")` gains `slug`, `loaded` and `has_files`, and its `filter` matches slug, name and description, so the model can still find a skill whose description was dropped from the index or that the user asked for by name.

**Resources (tier 3).**
Reference files are read with `agent_file_read` at `skills/<slug>/references/<file>`; scripts run in the container (§5).
Nothing from `references/` or `assets/` enters context until the model asks.

**Unified injection.**
All activation sources produce one per-turn set of active skills: skills loaded in the chat ∪ the active mode's skills ∪ the message's skills (`/skill`, hotkey, API) ∪ a job's skills.
Their bodies are injected **once**, deduplicated, in a new `SegmentKindSkills` developer segment placed right after the mode segment, each wrapped in `<skill name="slug">`.
`EnrichUserMessageWithRituals` stops appending bodies to the user text (the message keeps its `rituals` link for display), and `formatMoodRitualsForContext` stops rendering bodies; the mode block says only "Skills preloaded by this mode: a, b".
The segment shows up as its own row in the Context X-ray.

`/skill`, hotkeys and the API's `rituals` field stay **message-scoped**: the skill is active for that message's turn (every loop round of it) and is not written to the chat's loaded set.
The user asked for it once, so it costs its body once; what changes is that the body now arrives in the skills segment with its name instead of appended to the user text.
A skill the user invoked earlier is still in the index on the next turn, and the model has seen its body, so when the task continues the model is expected to load it itself with `load_skill`.
To make that likely, the index marks such skills `(used earlier in this thread)`, read from the thread's `chat_message_rituals` links.
Mode and job skills are preloaded for as long as the mode is active or the job runs, as today, since the user chose them.

### 3. Bundle files live in the gallery VFS, under `skills/<slug>/`

A skill's files are `file_attachments` rows, as #406 makes agent files, with two additions to that PR's model:

- `origin` is a **string enum** `user | agent | skill` (the models layer already has `FileOrigin`; #406 stores a bool, see §12), and
- a nullable `skill_id` edge to `rituals` with `ON DELETE CASCADE`.

Rows with `origin=skill` sit at folder `skills/<slug>/<subdir>` with the file's own name, so the spec's layout is the path: `skills/pdf-forms/scripts/fill.py`, `skills/pdf-forms/references/FORMS.md`.
Gallery folders already allow nested paths (depth 8, 64-rune segments, lowercased), which fits skill names and the three conventional subdirectories.
`SKILL.md` itself is **not** a row: it is rendered from the ritual columns when exported or mounted, so there is one source of truth.
Opening `skills/<slug>/SKILL.md` in the gallery or with `agent_file_read` returns that rendering; the path resolver treats it as a virtual file.

Rules for skill files:

- **Read-only to agents and to the gallery's move/rename.** They are managed through the skill editor (upload, replace, delete) and by import. `origin=skill` rows are excluded from `galleryFiles`' top-level views, from `MoveFileAttachmentsToFolder` and `RenameGalleryFile`, and from upload quotas; the gallery shows the bundle under the skill instead.
- **Any content type.** Import bypasses `agent_file_write`'s text allowlist, so `assets/` can hold images, fonts, spreadsheets and templates. `agent_file_read` still refuses binaries; they are for the container and for export.
- **Never chunked or embedded.** `find_context` fetch reads them straight from the object store, the same branch #406 adds for agent files (keyed on "unchunked text file", not on the agent flag).
- **Limits:** 200 files and 30 MB uncompressed per bundle (the Claude API's bundle limit), 10 MB per file.
- Object keys: `users/{uid}/skills/{skill_id}/{objectID}_{name}`, so account deletion purges them with everything else.

The `skills/` top-level folder is **reserved**: `agent_file_write` and the gallery refuse to create, move or upload there except through the skill editor and import.

### 4. Loaded skills persist per chat and unload at checkpoints, like tools

A new table `chat_skill_states` (`chat_id`, `ritual_id`, `loaded_at`, unique on the pair) holds the skills the agent loaded in a chat.
It is read where `prepareTurnLoadableTools` reads the loaded built-in tools, into `chatCtx.loadedSkills`, and written only by `load_skill` and `unload_skill`; message, mode and job activations never touch it.
As with tools, the in-memory set is updated on load so the body is injected from the **next loop round of the same turn**.

At a checkpoint, `unloadToolsUnusedInSegment` also unloads skills that were not used in the segment.
A skill counts as used when the segment holds a `load_skill` call naming it, a call to a tool it brought in (`allowed_tools` or a tool of one of its connectors), or a tool call that touched `skills/<slug>/` (`agent_file_read`, `load_file_to_container`, `shell_exec` with the path in its command).
A message activation in the segment does not count: it was never loaded state, and the index nudge covers re-loading.
The checkpoint summary prompt is told which skills were active so the summary names them, and the first user turn after a checkpoint carries the note "Skills that were loaded before the checkpoint: a, b. Load them again if the task continues."
Mode and job skills are not affected: they are preloaded each turn, not loaded state.

A skill that falls out of scope (its personality binding changed, or it was deleted) stays in the table but is neither injected nor listed; the row is harmless and is dropped on the next unload sweep.

### 5. Scripts run in the conversation's container (private build)

Script execution stays where code execution already is: the containers4agents workspace behind `shell_exec`.
Public builds without a container get the text and reference parts of every skill, and the `load_skill` result says "scripts in this skill cannot run in this deployment" with the skill's `compatibility` text.

In the private build, the bundle of every active skill is **hydrated lazily** into `/workspace/skills/<slug>/`:

- `ensureC4AContextFiles` (the per-`shell_exec` upload plan) adds the active skills' files to its selection, uploading what the manifest lacks, with the rendered `SKILL.md` first. Each upload is recorded through `recordC4AInput`, so skill files are never exported back as reply attachments.
- Because the selection is derived from persisted state (`chat_skill_states`, the mode, the job), a container that was pruned or a server that restarted re-hydrates on the next `shell_exec` with no extra bookkeeping.
- `manifestPathIsExportable` ignores `skills/`, `__pycache__/`, `.cache/` and `*.pyc`, so a script's own files do not come back either.
- `load_skill`'s result and the developer guidance tell the model: "Scripts of loaded skills are at `/workspace/skills/<slug>/scripts/`; run them with `shell_exec` from `/workspace/skills/<slug>`; only their output enters the conversation."

Constraints the skill author must know, surfaced in the skill editor and on Nexus listings: the container has **no network and no package installation**, so scripts use the runtime image's toolchain (Python 3.12 with pandas, openpyxl, python-docx, python-pptx, pypdf, reportlab, Pillow, matplotlib and friends; pandoc, LibreOffice, poppler, qpdf, jq).
`compatibility` is where a skill states what it needs; a `whatiff-runtime: c4a-2026-10` metadata key lets Nexus badge skills that were verified against the current image.
Exec limits are the existing ones (one exec at a time, timeouts, 4 MiB stdout).

### 6. `allowed_tools` names tools the skill needs, and loads them

The spec marks `allowed-tools` experimental and harnesses disagree on whether it is a permission grant (Claude Code: one turn) or advice (Factory, Copilot SDK, Vibe).
We treat it as **"load these with the skill"**, which is the only thing it can safely mean in a product where the user controls the tool set per thread:

- Names that are loadable built-ins offered in this chat (`web_search`, `fetch_page`, `generate_image`, `shell_exec`, `agent_file_read`, …) are loaded when the skill loads.
- Names the user has disabled in the thread, or that this deployment lacks, are ignored and listed in the `load_skill` result as "not available here".
- It never widens what the user allowed. The thread's `disabled_tools` and the sandbox policy still win.

`allowed_tools` is the **only** path by which loading a skill loads tools automatically; connectors contribute an inventory (§2), not loaded tools.
**Follow-up pass:** accept connector tool names (`mcp__<connector>__<tool>`, resolved against the skill's own connectors) in `allowed_tools`, so a skill author can say which three of a connector's forty tools the skill needs and have them load with it. The editor's Tools panel then offers the skill's connector tools as checkboxes. Not in the first pass because it needs the connector's discovery result at edit time and a stale-name policy.

Harness-specific entries such as `Bash(git:*)` are kept on the row for round-tripping and ignored at runtime.

### 7. Import and export are the standard's shape

- **Import:** `POST /ritual/import` takes a zip (the skill directory at the zip root or one enclosing folder) or a multipart set of files with relative paths. The server parses `SKILL.md` frontmatter leniently as the spec's client guide recommends (missing or over-long name → derived slug plus a warning; missing description → refused; unknown keys kept in `metadata`), sanitizes text (§10), creates the ritual and its `origin=skill` rows. Re-importing into an existing skill replaces its files. The skill editor gets a "Import SKILL.md bundle" drop target and a per-skill "Files" panel (upload, replace, delete, view).
- **Export:** `GET /ritual/{id}/export` returns `<slug>/SKILL.md` plus the files as a zip. `SKILL.md` carries `name: <slug>`, `description`, `license`, `compatibility`, `allowed-tools` and `metadata` with `display_name`, `whatiff-hotkeys`, `whatiff-connectors` (names) and the invocation flags under `whatiff-` keys. What comes out imports into Claude Code, Codex, Cursor and the others unchanged.
- **Account backup** gains the new columns, the files and the mode and job links (all missing today).
- **Agent-authored skills** reuse #406's ownership rule later: an agent may create a skill and edit one it created (its personality's), never a user's. That is the "skill-creator" loop every harness is converging on and is scheduled after the basics (§13).

### 8. Nexus carries the whole bundle

The skill projection grows from `{name, description, content, hotkeys}` to the full SKILL.md field set plus `files: [{path, sha256, content_type, size}]`, with the file bytes stored as **version assets** exactly like personality expression images.
Install writes the files into the installer's VFS under `skills/<slug>/` with `origin=skill` and the new `skill_id`; upgrade replaces them; the projection hash covers the file hashes, so a locally edited script reads as a local modification like an edited prompt.
Slug collisions on install get a numeric suffix and the listing's slug stays in `metadata.whatiff-nexus-slug` for dependency resolution.
Moderation shows scripts as a diff against the previous version and runs the scan in §10; a skill with scripts is badged on its listing.
Job dependencies on skills are unchanged.

### 9. What the user sees

- **Skill editor:** slug (auto-filled), description with the 1,024 cap and a "when to use it" hint, body with a token counter and the 5,000-token advisory, `license`, `compatibility`, the two invocation toggles, `allowed_tools` as chips of this deployment's tools, the Files panel, Import and Export. Connectors and hotkeys as today.
- **Thread:** the Tools panel's new "Skills" row lists loaded skills with Unload, next to loaded tools. The Context X-ray shows the skills index and the active-skills segment as rows, so a user can see what a skill costs. Tool-call timeline entries for `load_skill` render as "Loaded skill *name*".
- **Composer:** `/skill` and hotkeys keep their picker and chips; the chip tooltip says the skill stays loaded in the thread until it is unloaded or unused at a checkpoint.
- **Mode and job editors:** unchanged, now backed by many-to-many links (§11).

### 10. Trust and safety

Skills are prompts and code that run with the user's permissions, and Nexus makes them shareable; the recent ClawHub and skills.sh audits found prompt injection in most malicious skills and invisible-Unicode smuggling in many.

- **Sanitize on write:** import, create and update strip Unicode tag characters, zero-width and bidi controls from `name`, `description` and `content`, and reject XML-looking tags in `name` and `description` (the Claude API's rule). Files are stored as uploaded.
- **Bound everything:** description 1,024 chars, body 64 KB hard, bundle 30 MB and 200 files, index 1% of context, `load_skill` result capped like other tool results and classified in `toolContextPolicyFor` (persist 10 turns, like `agent_file_read`).
- **Frame the content:** bodies are injected inside `<skill name="…">` in a developer segment with a one-line preamble that skill instructions describe how to do a task and do not change who the user is or what the system prompt allows.
- **Scripts only in the container:** no network, no installs, resource limits, per-conversation workspace, never on the API host. Output is capped and comes back as a tool result like any `shell_exec`.
- **Nexus:** scripts and body are scanned at submission for injection patterns and smuggled characters; results are shown to moderators; installed skills are version-pinned and local edits are detected (existing behaviour).
- **Sandboxed threads** see no skills and no `load_skill`, as they see no account content today; `sandboxedToolSurface` classifies the new tools `SandboxNever`.

### 11. Fix the mode and job links

`Mood.rituals` and `AgentJob.rituals` become true many-to-many edges (`edge.From` on `Ritual` with `Ref`), producing `mood_rituals` and `agent_job_rituals` join tables.
A migration copies the current FK column values into the join tables and drops the columns.
Attaching a skill to a second mode or job then no longer detaches it from the first, which the editors silently do today.
This ships first, since every later step assumes skills are reusable.

### 12. Changes proposed to the in-flight VFS work (#406, what-iff-private#225)

Posted 2026-10-31 as a comment-only review on #406 and a comment on #225; the author has agreed to the #406 changes, so step 3 of the delivery plan can assume them.

Cheap before those PRs merge, costly after:

| # | Change | Why |
|---|---|---|
| 1 | Store `origin` as a string enum (`user`, `agent`; `skill` comes with this ADR) instead of `agent_authored bool` on `file_attachments`. Keep `AgentAuthored` as a derived accessor. | The models layer already has `FileOrigin`; a bool means a second migration and rewriting `SwapAgentFileObject`, `CountAgentFiles`, the `Origin` filter, `classifyFileAttachmentSource`, `readableAsText` and `fetchFile` once `skill` arrives. |
| 2 | Export the path resolver and text reader from `internal/agent/tools` (`splitAgentFilePath`, `resolve`, `readText`, `filePath`), or move them to `datastore`/`storage`. | #225 already re-implements `resolve` for `load_file_to_container`; `load_skill`, the `SKILL.md` rendering and the container hydration would be copies three to five. |
| 3 | Key the `find_context` direct-read branch (`fetchAgentFile`) on "text file with no chunks", not on `AgentAuthored`. | Skill files are unchunked too; one condition instead of a growing `||`. |
| 4 | Reserve the `skills/` top-level folder in `agent_file_write` create, copy and move. | So agents cannot squat the namespace before skill rows exist. One check in `newFile` and `move`. |
| 5 | `readText` returns `("", nil)` when `ResolveAttachmentTextContent` fails, and the caller reports "The file is empty." | Return an error; it will otherwise mask a broken skill file as empty. |
| 6 | Say in the `list view="tree"` description that the tree covers the current page, or fetch the whole `folder` prefix for tree view. | Skill bundles are small, but a full library tree will be misleading otherwise. |

Follow-ups we take ourselves (not asks on the teammate):

- `skill_id` edge and `origin=skill` handling in `galleryFiles`, move/rename and quotas (§3).
- Generalize #225's single-file copy into a subtree hydration used by `ensureC4AContextFiles` (§5); `recordC4AInput` is reused as is.
- Chain `additionalGeneratedAttachmentsForChat` and `onToolUseGeneratedAttachmentsForChat` in `c4a_shell_exec.go`, which are assigned without preserving a previous hook today; the skills overlay file must not clobber them.
- #269 (export should include entities and workspace files) now also covers skill files.

## Architecture

Per-turn flow after this change (chat turn; jobs and relays share it):

```
turn start
  ├─ resolve active mode ──────────────► mode.skills (preloaded)
  ├─ message.rituals (/skill, hotkey, API) ─► active for this turn only
  ├─ read chat_skill_states ───────────► chatCtx.loadedSkills
  ├─ active = loaded ∪ mode ∪ message ∪ job     (deduped, sorted by slug)
  ├─ policy.ritualIDs = active ────────► connectors in MCP scope (unchanged seam)
  ├─ developer context
  │     ├─ Skills index: in-scope − active − disable_model_invocation (1% budget)
  │     └─ SegmentKindSkills: <skill name=slug>body</skill> per active skill
  └─ tool specs: load_skill / unload_skill present iff skills in scope
loop round N
  ├─ load_skill(slugs) → chat_skill_states, chatCtx.loadedSkills,
  │     allowed_tools loaded, agent_file_read loaded (connectors: in scope, not loaded)
  │     returns body + <skill_resources> + connector tool inventory
  ├─ load_tools(mcp_server_id, names) → the connector tools the task needs
  ├─ agent_file_read("skills/<slug>/references/x.md")      (tier 3, text)
  └─ shell_exec("cd /workspace/skills/<slug> && python scripts/run.py")
        └─ ensureC4AContextFiles hydrates active skills' files first (private)
checkpoint
  └─ unload skills (and tools) unused in the segment; summary names them
```

Storage:

```
rituals ──1:N── file_attachments(origin=skill, skill_id, folder="skills/<slug>/…")
   │ M2M mood_rituals, agent_job_rituals, ritual_mcp_servers, chat_message_rituals
   └── chat_skill_states(chat_id, ritual_id, loaded_at)
SKILL.md = render(ritual)   ← import parses, export/mount render
```

Where the code goes:

| Piece | Package |
|---|---|
| Frontmatter parse/render, slug rules, sanitizer, zip import/export | new `internal/skills` (pure, no DB) |
| `load_skill` / `unload_skill`, index text, `SegmentKindSkills`, checkpoint unload | `internal/agent` (next to `tool_loading.go`, `checkpoint_tool_unload.go`), `internal/agent/tools` catalog |
| `chat_skill_states`, ritual columns, M2M edges, `skill_id` on files | `ent/schema`, `internal/datastore` |
| `/ritual/import`, `/ritual/{id}/export`, `/ritual/{id}/files` | `internal/handlers/ritual`, `openapi.yaml` |
| Skill editor Files panel, import/export, Tools panel Skills row, Context X-ray row | `web/app` `features/ritual`, `features/chat` |
| Container hydration, export filter, guidance | private `overlay/internal/agent/c4a_skills.go` |
| Projection, version assets, install, moderation scan | private `overlay/internal/nexusproj`, `overlay/internal/datastore/nexus_*` |

## Alternatives considered

- **Keep injecting every attached skill in full and only add files.** Cheapest, but it leaves the two gaps the standard exists to close: the model cannot find skills it was not handed, and every skill costs its body on every turn. Rejected.
- **Put the skills index inside `load_skill`'s tool description** (OpenCode's approach) instead of the developer context. It keeps the system prompt clean, but the description is part of the tool definition token estimate and is re-sent with the tools on every call; our developer context already carries the loadable-tools list, and the Context X-ray can show the index as a row. Rejected for consistency with `loadableToolsDeveloperContext`.
- **Extend `load_tools` with a `skills` argument** rather than a new tool. The persistence mechanics are the same, but the result differs (a body and resource list, not a loaded-names list), and models trained on the standard look for a skill tool. A separate `load_skill` it is; its handler shares `loadBuiltinTools`' helpers.
- **A `SKILL.md` row per skill in the VFS.** It makes the bundle a plain folder, but it is a second copy of the name, description and body that the editor and Nexus would have to keep in step. Render it instead (§3).
- **Skill files in their own table.** Cleaner ownership rules, but it would be a second file system beside the one #406 is building, invisible to `list`, the gallery, `agent_file_read` and `load_file_to_container`. ADR 0x034 rejected the same thing for agent files.
- **Eager container mount at `load_skill`.** Simpler to reason about, but it fails silently when the container was pruned or the server restarted, and it costs an upload for skills whose scripts are never run. Lazy hydration from persisted state (§5) is the same mechanism context files already use.
- **`allowed_tools` as a permission grant.** We have no per-turn permission prompts to grant through, and the user's thread-level tool toggles must win. "Load these" is what the field can mean here (§6).
- **Running scripts on the API host or in a per-call throwaway container.** No isolation, or no persistent workspace for multi-step work. The existing per-conversation container has both.
- **Make `/skill` and hotkeys load the skill for the thread**, as harness slash commands do. Rejected: the user's gesture is per message, and once the agent can load skills itself and has seen the body, re-loading is its call; the index nudge (§2) and a "used earlier" marker make that cheap. Appending the body to the user text, the actual problem with the current path, is fixed by the skills segment regardless.
- **Load every connector tool when a skill loads**, as Amp does for skill-scoped MCP servers. Rejected: a connector may expose dozens of tools and the skill needs a few; the skill's connector inventory in the `load_skill` result plus `load_tools` keeps the choice with the model, and `allowed_tools` (with connector names in the follow-up) is the explicit opt-in for auto-load.
- **Auto-select skills with a utility model**, as modes are selected. The index plus the model's own judgment is what every harness does; a second model call per turn is the thing progressive disclosure avoids. Not now; mode auto-selection stays.

## Consequences

- Models can find and use skills they were not handed, and a thread with many skills attached pays about a line per skill instead of every body.
- Skills become portable in both directions with the rest of the ecosystem; Nexus listings become bundles with code.
- Scripts give skills real capability in the private build; public builds degrade to text and references, and say so.
- `/skill` keeps its "this message" semantics; what the user notices is that the model may now load the same skill itself on a later turn, which the Tools panel's Skills row shows.
- A schema migration touches `rituals`, `file_attachments`, `moods`/`agent_jobs` links, and adds `chat_skill_states`; the M2M migration must run before anything else lands.
- The developer context grows by the index; for accounts with hundreds of skills the budget truncates descriptions, and the model falls back to `list(kind="skills")`.
- Every surface that renders skill text (message bubbles, Nexus listing pages, the editor) now renders Markdown bodies that may reference files; links to `skills/<slug>/…` should open the file viewer.
- Three more tools in the sandbox surface test; two more context segments in the X-ray.

## Delivery plan

Each step is a PR (or a public and private pair) that leaves the product working.

| Step | Repo | Content | Depends on |
|---|---|---|---|
| 0 | public | §11 M2M migration for mode and job links; openapi gaps (`/agent-job/{id}/rituals`, `mcp_server_ids` in ritual bodies) | — |
| 1 | public | §1 columns, slug rules and sanitizer (`internal/skills`); editor fields; description cap; import/export of **text-only** bundles | 0 |
| 2 | public | §2 and §4: index, `load_skill`/`unload_skill`, `SegmentKindSkills`, unified injection (`/skill` stays message-scoped), `chat_skill_states`, `(used earlier in this thread)` index marker, checkpoint unload, Context X-ray and Tools panel rows | 1 |
| 3 | public | §3 bundle files: `skill_id`, `origin=skill`, reserved folder, Files panel, full import/export, `agent_file_read` auto-load, `list(kind="skills")` fields | 2, #406 merged with §12 items 1–4 |
| 4 | private | §5 container hydration and export filter; §6 `allowed_tools` loading of `shell_exec`; guidance | 3, #225 merged |
| 5 | private | §8 Nexus projection with version assets, install/upgrade, moderation scan and badges | 3 |
| 6 | both | §7 agent-authored skills (create/edit own skills), account backup of bundles and links, usage analytics per skill (loads, last used) in the editor | 3–5 |

Steps 1 and 2 close the two gaps the user asked about with no dependency on the VFS PRs; steps 3 onward add files and scripts on top of them.

## Decisions

Calls made in this design that are not forced by the standard or the code.

Confirmed 2026-10-31:

1. `/skill`, hotkeys and the API's `rituals` field stay **message-scoped**. They are not written to the chat's loaded set; the index marks skills used earlier in the thread so the agent re-loads them itself when the task continues.
2. `load_skill` **does not load connector tools**. It brings the skill's connectors into scope and returns their tool inventory (the `list(kind="mcp_servers")` shape for those connectors); the model picks with `load_tools`.
3. `allowed_tools` means **"load these with the skill"**, never a permission grant, and is the only automatic load a skill triggers. Connector tool names in `allowed_tools` are a follow-up pass (§6).
4. Agent-loaded skills **unload at checkpoints when unused**, mirroring tools, with the summary naming them.
5. `SKILL.md` is **rendered, not stored**. We support the file's semantics for import, export and the container mount, but the design is built around the ritual row, which stays the source of truth.
6. Skill files are `file_attachments` rows with `origin=skill`, **read-only to agents and to gallery move/rename**, managed through the skill editor.
7. The skills index is budgeted at **1% of the context window** and degrades by dropping descriptions, never slugs.
8. The mode and job links are migrated to **many-to-many** first (§11). This is a long-standing bug that was never prioritised while the skills system was behind; it ships as step 0.
9. The six #406 changes in §12 land before step 3. Posted 2026-10-31 as a non-blocking review; GoriVix has agreed to make them.

## Not in this change

- Hooks inside skills (Claude Code's `hooks` frontmatter), dynamic `!`command`` injection and `$ARGUMENTS` substitution. Our skills are invoked by loading, not by slash commands with arguments; revisit if users ask for parameterized skills.
- Path- or file-triggered activation (`paths` globs). There is no working directory in a chat; the index covers discovery.
- Forked-context execution (`context: fork`). `run_subagent` with `skill_ids` already is that, and it keeps its full-content preload.
- Installing skills from a URL or GitHub repo server-side. Nexus is the distribution channel; the zip import covers the rest.
- Connector tool names in `allowed_tools` (the §6 follow-up); the first pass loads built-ins only.
- Skill-to-skill dependencies. The spec has none; `metadata` is the place if one is needed.
- Per-skill evals (should/shouldn't-trigger description tuning). Worth adding once usage analytics exist (step 6).

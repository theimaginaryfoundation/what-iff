# ADR 0x043: The file workspace: viewing and editing files inside the app

- **Status:** Accepted 2026-11-06 (design only; nothing implemented yet)
- **Date:** 2026-11-06
- **Deciders:** What Iff maintainers
- **Builds on:** the gallery as a virtual file system and agent file tools ([ADR 0x034](0x034-agent-files-and-entity-wiki.md)), skill bundles and their container hydration ([ADR 0x035](0x035-agent-skills.md) §5), the read-only gallery file viewer (`features/gallery/components/file-viewer`), and the private build's containers4agents (c4a) integration (`shell_exec`, `load_file_to_container`).
- **Private half:** what-iff-private `docs/design/2026-11-06-file-workspace-c4a-sync.md` (container sync).

## Context

Working with files in a conversation is a round trip today.
A user uploads a file, the agent reads or transforms it, the result comes back as an attachment, and the user downloads it, opens it somewhere else, changes it, and uploads it again.
Nothing in the app lets the user *see* what the agent is working with, or change a file in place and have the agent see the change.
The pieces that exist:

| Today | Where |
|---|---|
| A read-only viewer in the gallery: text and code as plain text, Markdown and CSV rendered, PDF through the browser's own viewer (so not in the Android WebView), images. Office files, archives and notebooks get a download button. The class comment says editing is out of scope. | `features/gallery/components/file-viewer.component.ts`, `helpers/file-preview.helpers.ts` |
| In chat, a non-image attachment is a pill that downloads on click. Image attachments show a thumbnail and a download. Nothing opens in the app and nothing links to the gallery. | `features/chat/components/message-content/` |
| Users can list, upload, download, rename, move and delete files. **Users cannot overwrite a file's bytes through the API.** Only `agent_file_write` (agent-owned files) and the skill-bundle upload write content, both by uploading a new object and swapping `s3_key` with a conditional update. | `internal/handlers/{fileattachment,imagegallery}`, `internal/datastore/agentfile.go` (`SwapAgentFileObject`, hard-coded to `origin=agent`) |
| A chunked upload that changes content has no re-chunk path: `CreateFileChunks` is a no-op once `chunk_status` is set and nothing deletes `file_chunks`. | `internal/datastore/filechunk.go` |
| "The files in this conversation" is not a stored set, a function, or an endpoint. The nearest rule is the private build's `selectC4AContextAttachments`, run lazily on every `shell_exec`: attachments on every message since the last checkpoint, the `attachment_id`s in `find_context` results in those messages, attachments on the last 10 user messages, and files other tools produced this turn, deduplicated by file name (newest wins) into `/workspace/<name>`. Agent-authored files and the rest of the gallery are only reached through `load_file_to_container`. | what-iff-private `overlay/internal/agent/c4a_shell_exec.go` |
| Once a path is in the container manifest it is never re-uploaded, so an edit to a file already hydrated never reaches the container. Skill bundles do it right: SHA-256 against the manifest, re-upload on change, `rm -f` on removal. | `c4a_shell_exec.go` (`buildC4AContextUploadPlan`), `c4a_skills.go` |
| The chat side panel has four hard-coded tabs (Scratchpad, Memories, Tools, Context). The layout has one breakpoint: 1024px and up docks a 320px panel, below that a bottom sheet. There is no tablet mode, no breakpoint service, and no docked side-by-side or full-screen overlay primitive (`ui-modal` tops out at `max-w-4xl`). | `features/chat/components/context-panel/`, `layout/app-layout.component.ts` |
| The web app is Angular 22 on esbuild with a 2.5 MB initial-bundle budget and a 30 kB per-component style budget, no web workers, no CSP, no service worker, and no syntax highlighter. The same bundle runs in the Capacitor Android WebView, so workers and wasm must be served from the app's own origin. The repo is Apache-2.0 and nothing checks dependency licenses. | `web/app/angular.json`, what-iff-private `mobile/` |
| Replies arrive as one POST response; the typewriter effect is client-side. There is no server push channel, so a panel that must reflect agent writes can only refresh on turn completion, on return to the app, and on its own actions. | `core/services/message.service.ts`, `chat-page.component.ts` (`onReturnToApp`) |

The goal is for a conversation to feel like working *with* the agent on a shared set of files: see what it sees, open any of it in place, change it, and have the change reach the agent (and its container) without leaving the thread.

## Decision

### 1. One file workspace, hosted in three places

A single **file workspace** owns "which file is open, in which mode, in which layout".
It is hosted by the chat page (next to the thread), by the gallery (in place of today's inline viewer), and by a deep link (`?file=<id>` on either route, which the chat already uses for `?galleryImageId=`).
The same host component renders the same viewer and editor components everywhere, so a file looks and behaves the same whether it was opened from a message pill, the Files tab, or a gallery tile.

Layout follows the viewport, with the thresholds below (the app's only breakpoint today is 1024px; this introduces a tablet band):

| Viewport | Default | Editing | Notes |
|---|---|---|---|
| **Large** (≥ 1280px) | Docked split: the workspace takes the right column beside the thread, resizable (min 40% of the viewport), with an expand to full-screen and a collapse back. The context panel's rail stays; opening a panel tab while a file is docked shows that tab as a 320px flyout over the workspace. | Yes | The 1024–1279px band is too narrow for thread + 320px panel + an editor, so it behaves as medium. |
| **Medium** (768–1279px) | Full-size overlay over the chat (a new `ui-overlay` primitive: focus trap, Escape, scroll lock; the existing `ui-modal` is capped at `max-w-4xl`). | Yes | Tablets. The chat stays mounted underneath; closing returns to it. |
| **Phone** (< 768px) | Full-screen, replacing the thread view (same overlay, no editor chrome). | **View only** | Editors are hidden, not merely disabled: Monaco and the office editors do not work on touch keyboards, and the phone build has no room for a toolbar. |

The open file and layout mode are remembered per chat in local storage, like the active context-panel tab.

### 2. Viewers and editors are a registry of lazily loaded components

`file-preview.helpers.ts` grows from a `FilePreviewKind` into a **registry**: each kind names a viewer component, an optional editor component, a `loadEditor()` dynamic import, and capability flags (`view`, `edit`, `phoneView`).
Every heavy library lives behind its kind's `import()`, is cached like `loadKatex()` in `latex-math.helpers.ts`, and never enters the initial bundle.
Workers and wasm (Monaco language workers, the PDF.js worker, Pyodide) are copied to `assets/` by `angular.json` globs and loaded by relative URL, because the Capacitor build serves the bundle from local files.

| Kind | Extensions | View | Edit | Library (license) |
|---|---|---|---|---|
| code / text | everything in `TEXT_EXTENSIONS`, `.json`, `.yaml`, … | Monaco read-only (`colorize` to static HTML on phones) | Monaco editor; languages loaded per file type; autocomplete from Monaco's built-in services for TS/JS/JSON/CSS/HTML and word-based elsewhere | `monaco-editor` (MIT) |
| markdown | `.md .markdown .mdx` | Rendered (the chat's `<markdown>` + `renderLatexMath`) with a raw toggle | Monaco with a live rendered pane (split on large, tabbed on medium) | `monaco-editor`, `ngx-markdown` (already present) |
| image | png, jpeg, gif, webp | As today | Crop, rotate, resize, annotate, filters; saves as PNG/JPEG/WebP | Filerobot Image Editor, vanilla adapter (MIT) |
| pdf | `.pdf` | PDF.js viewer: pages, text layer, search, zoom. Replaces the browser-native viewer and so works in the Android WebView. | PDF.js annotation editor layer: highlight, free text, ink, stamp; `saveDocument()` writes the annotations into the file | `pdfjs-dist` (Apache-2.0) |
| spreadsheet | `.xlsx .csv .tsv` | Grid, one tab per sheet | Grid editing with formulas; CSV saved back as CSV | Univer Sheets (Apache-2.0) for the grid; ExcelJS (MIT) to read and write `.xlsx`, because Univer's own xlsx import/export is a paid feature |
| document | `.docx` | Rendered to HTML with page layout | A rich-text editor over an HTML round trip, with a fidelity warning (see §7) | `docx-preview` (MIT) to view; `mammoth` (BSD-2) + ProseMirror/TipTap (MIT) + `docx` (MIT) to edit |
| slides | `.pptx` | Rendered slides (SVG/HTML) | Text in place: the editor opens the OOXML (`jszip`, already present) and rewrites text runs; layout and media untouched | `pptx-glimpse` or `pptx-browser` (MIT) to view, chosen in the spike |
| notebook | `.ipynb` | Cells with rendered Markdown, code and outputs (text, sanitized HTML, images, JSON) | Edit cells, add/remove/reorder, change cell type; saved as nbformat 4. **Run** opens the notebook in an embedded JupyterLite (§8). | our own component on `<markdown>` + Monaco; JupyterLite + `jupyterlite-pyodide-kernel` (BSD-3) |
| none | archives, unknown binaries | Badge and download, as today | | |

No GPL, AGPL or commercially licensed dependency is allowed; SuperDoc (docx, AGPL), OnlyOffice (AGPL, server), Handsontable and Pintura (commercial) were ruled out on that basis.
A license check is added to `frontend-pr-validation` so the rule holds without review.

### 3. Save, Save As, Rename, and what "the same file" means

A **content version** is added to `file_attachments`: `content_version` (int, starts at 1) bumped by every content write, with `updated_at` set and `size_bytes` filled.
The file viewer carries the version it loaded, and every save sends it back.

| Action | API | Semantics |
|---|---|---|
| **Save** | `PUT /file-attachment/{id}/content` (bytes; `If-Match: <content_version>`) | Uploads a new object and swaps the row's `s3_key` with the conditional update that `SwapAgentFileObject` already does for agent files, generalized to any file the user owns. A version mismatch is `412`, and the editor offers *Reload*, *Overwrite* or *Save as*. |
| **Save As** | `POST /file-attachment` (bytes, `folder`, `name`, optional `chat_id`) | A new file in the gallery. With `chat_id`, it is also attached to the conversation: a reference row on a system message in that chat (so it meets the §4 rule), and in the private build seeded into the live container. |
| **Rename** | `PATCH /image-gallery/{id}` (exists) | Hardened: refuses a path clash in the folder, refuses skill-bundle files, updates `file_type` when the extension changes, sets `updated_at`. |

"The same file" is the **object graph**, not the row.
A message attachment is often a reference row that shares `s3_key` with the gallery original (chat reuse and `find_context` copies).
A save therefore moves every row that shares the old key **and** is not `context_only` to the new key in one update, so the gallery file and its chat copies stay one file and chat copies never surface in the gallery as separate files (today's `excludeReferenceCopies` relies on them sharing a key).
`context_only` rows keep the old object: they record what a reply saw, and the old object is released only when nothing references it (`ReleaseReplacedObject`, unchanged).

Saving a user upload that was chunked deletes its `file_chunks`, resets `chunk_status` and re-runs `pipeline.ProcessAndStore`, so `find_context` serves the new text.
Agent files stay unchunked.
Saving an image regenerates its thumbnail.
Content URLs gain `?v=<content_version>` so the hour-long `Cache-Control` never shows stale bytes.

The ownership rule of ADR 0x034 §3 is unchanged: the user owns everything and may edit any of their files, including agent-authored ones; agents still edit only what they created.
There is no version history in v1: Save overwrites and Save As is the way to keep a copy.

### 4. "Files in context" is a public rule with a public endpoint, and the Files tab shows it

The selection rule moves out of the private `selectC4AContextAttachments` into public `internal/agent/contextfiles.go`:

```go
// ContextFilesForChat lists the files an agent working in this chat is expected to see.
func (a *Agent) ContextFilesForChat(ctx, chat) ([]ContextFile, error)

type ContextFile struct {
    Attachment    *models.FileAttachment
    WorkspacePath string          // where a container would mount it, e.g. "report.csv" or "skills/<slug>/SKILL.md"
    Reasons       []ContextReason // attached_recent, attached_since_checkpoint, recalled, produced_this_turn, agent_written, skill_bundle
}
```

The rule is the existing one (messages since the checkpoint, `find_context` results in them, the last 10 user messages' attachments, this turn's tool outputs, active skill bundles), with one addition: **agent-authored files created in this chat** (`origin=agent`, `created_by_chat_id = chat`).
Without them, a file the agent just wrote with `agent_file_write` would be invisible in the tab that claims to show what the agent is working with.
Deduplication stays "newest wins per workspace path", and the tab shows the shadowed duplicates greyed out with the reason, so the container's name-collision rule stops being invisible.
The private build keeps calling this function for hydration, so the tab and the container cannot drift.

`GET /chat/{chatId}/context-files` returns the list.
A private seam (`containerStatusForChat`, nil in public, in the style of `skillScriptsRunnableForChat`) lets the private build add, per file, whether a live container holds it and whether its copy is current.

The **Files** tab is the fifth context-panel tab (type union, nav, `@switch`, label, the chat-page rail, plus the specs).
Each row shows the name, the type badge from `fileBadge`, its origin (uploaded, agent-written, recalled, skill), and opens the workspace on click.
The tab has an *Add file* action (upload, or pick from the gallery; the composer's gallery picker is extended from images to all files for the same reason).
It refreshes when a turn completes, when the app regains focus, and after the workspace saves; a server push channel is later work (§10).

### 5. Chat affordances

Attachment pills and image cards in messages open the workspace on click, so a file the agent just produced is one tap away from the message that shows it, without opening the side panel.
The pill keeps its download: a download icon button on the pill itself (and in the viewer's toolbar), so the direct download users have today is not lost.
The composer keeps uploading immediately; a *Save As* from a chat-hosted workspace lands the file in the composer's pending attachments **and** attaches it to the chat as described in §3, so it is in context before the user sends anything.

### 6. Gallery

The gallery hosts the same workspace in place of `FileViewerComponent`, whose rename, move, delete, previous/next and "open its thread" actions move into the workspace toolbar.
Images keep the lightbox for browsing but gain an *Edit* that opens the workspace.
The import modal accepts any allowed file type, not only images.

### 7. Fidelity: office formats are edited honestly

Browser-side editing of `.docx` and `.pptx` without a commercial engine is lossy.
The rule is: **a viewer renders what the file contains; an editor says what it will lose before the first save.**

- **Spreadsheets** keep cell values, formulas, number formats, basic styles, merges and column widths. Charts, pivot tables, data validation and macros are dropped on save and the editor says so when the workbook has them.
- **Documents** round-trip paragraphs, headings, runs, lists, tables, images and hyperlinks. Tracked changes, comments, headers and footers, fields, and section layout are dropped. The editor defaults the first save of a lossy document to *Save As* and names the dropped features.
- **Slides** are text-in-place only in v1, which loses nothing. Adding, moving or resizing shapes is not offered.
- **PDF** annotations are written as real annotation objects by PDF.js, so other readers show them; the spike verifies this with the readers we care about.

### 8. Notebooks: our own cells, JupyterLite for running

The notebook viewer and editor is our own component: cells rendered with the chat's Markdown renderer and Monaco, outputs rendered by MIME type with sanitization, nbformat 4 read and written by hand (it is JSON).
This is what the Files tab, the phone view and the side-by-side layout need, and it keeps the notebook inside the workspace's save flow.

**Run** does not execute on our servers.
It opens the notebook in an embedded **JupyterLite** (an iframe under `/jupyterlite/`, built with `jupyterlite` and `jupyterlite-pyodide-kernel` and served as static assets), with a small JupyterLite extension that provides a contents drive backed by `postMessage` to the host page: the host supplies the notebook and persists saves through the same `PUT …/content` call.
JupyterLite is the full Lab UI, so it is a mode the user enters from the workspace toolbar, not the default view.
Known limit: JupyterLite needs a service worker for the kernel to read files from the drive with plain `open()`; the app has none, and the Capacitor WebView may not allow one, so *Run* is desktop-and-tablet web first.

`.ipynb` (`application/x-ipynb+json`) is added to the upload allowlist, with `.svg`, `.yaml`, `.tsv` and `.jsonl` which are also missing today.

### 9. Backend changes (public)

- `file_attachments.content_version` and `content_sha256`; `updated_at`, `size_bytes` and the hash set by every content write; older rows get `size_bytes` and the hash lazily on first read.
- A seam `onFileContentChanged` (nil in public) raised after every content write, rename and save-as, which the private build uses to push into live containers.
- `PUT /file-attachment/{id}/content`, `POST /file-attachment` (gallery create with optional chat attach), hardened rename; the gallery routes the web app already uses but `openapi.yaml` lacks (`GET/PATCH/DELETE /image-gallery/{id}`, `/import`, `/reference`) are documented in the same change, since the SDK is generated from the spec.
- `SwapAgentFileObject` becomes `SwapFileObject(ctx, userID, id, oldKey, newKey, opts)` with the origin check as an option; agent writes keep theirs.
- Re-chunking: `ReplaceFileChunks` deletes and re-runs the pipeline.
- `ContextFilesForChat` and `GET /chat/{chatId}/context-files`.
- Upload allowlist additions (§8).

### 10. The private build: containers stay private, sync is explicit

Detailed in the private design doc. In short: hydration uses `ContextFilesForChat` and compares SHA-256 against the container manifest the way skill bundles do (fixing the "never re-upload" gap); a save, rename or save-as from the workspace pushes to the chat's live container when there is one (rename deletes the old path); a gallery save pushes to every live container whose chat has the file in context; every push is recorded as an input so the end-of-turn sweep does not export it back as a new reply attachment; and the chat→container mapping is persisted so "is there a live container" can be answered without creating one.

## Alternatives considered

- **CodeMirror 6 instead of Monaco.** Lighter, works on touch, and easier to bundle. Monaco was chosen for its built-in language services (TypeScript, JSON schema, CSS, HTML completions) and because the user asked for it; phones are view-only anyway. If the esbuild/worker spike fails in the Capacitor WebView, CodeMirror is the fallback.
- **A hosted office engine (OnlyOffice, Collabora Online) for docx/xlsx/pptx.** Full fidelity, but AGPL or a server to run and pay for, and it would pull every edit through our infrastructure. Rejected.
- **One editor framework for all office types (Univer Docs and Slides).** Univer's document and slide editors are early and have no OOXML import; its sheet editor is solid. Only Sheets is used.
- **Embedding JupyterLite as the only notebook UI.** Simplest, but an iframe with its own look that cannot dock in the workspace, cannot render on phones, and would hold notebooks in browser storage instead of the gallery. It is kept for execution only.
- **Executing notebooks in the c4a container.** Real kernels, but it hosts execution, which the user ruled out for the in-app notebook. Agents already run code in the container when they need to.
- **Edits as a new file every time (no overwrite).** Safe, but it is the download-and-re-upload loop in disguise, and the agent would see a growing pile of versions. Overwrite with a version token and an explicit Save As instead.
- **Watching the server for file changes (SSE or WebSocket).** The right end state, but there is no push channel in the app today; a refresh on turn completion covers the agent-writes-while-I-watch case without new infrastructure. Deferred.

## Consequences

- Users can open, read and change every file in a conversation without leaving it, and the agent (and its container) sees the change on the next turn.
- The initial bundle does not grow; each viewer is a lazy chunk. The heaviest (Monaco ~2 MB, PDF.js ~1 MB, Univer ~3 MB, JupyterLite ~tens of MB with Pyodide) load only when a file of that kind is opened, and JupyterLite only on *Run*.
- Office editing is lossy and says so. Users who need full fidelity use the viewer and edit elsewhere, as today, until a better engine appears.
- The gallery and the chat share one viewer; `FileViewerComponent` is retired.
- `file_attachments` gains `content_version` by auto-migration; existing rows start at 1.
- The selection rule for container hydration becomes public code, and the private build's hydration gains an addition (agent-written files of the chat) and a fix (changed files are re-uploaded).
- There is no history: a bad save is a bad save. Save As is the escape hatch, and history is on the later list.

## Delivery plan

Stacked PRs on `design/file-workspace`, public unless marked. Each step is usable on its own.

| Step | Branch | Scope |
|---|---|---|
| 0 | `design/file-workspace` | This ADR, the private design doc, and four throwaway spikes: Monaco with esbuild workers in the Capacitor WebView; PDF.js annotation save checked in other readers; ExcelJS ↔ Univer round trip on real workbooks; `docx-preview` fidelity on a sample set. Results amend §2 and §7. |
| 1 | `feat/files-1-content-api` | §3 and §9: `content_version`, `PUT …/content`, `POST /file-attachment`, hardened rename, object-graph swap, re-chunking, thumbnails, upload allowlist, openapi gaps, SDK regen. |
| 2 | `feat/files-2-workspace-shell` | §1, §5, §6: `FileWorkspaceService` + host, docked/overlay/phone layouts, `ui-overlay`, registry, deep links; the existing viewers migrated (text, Markdown, CSV table, image) plus PDF.js view; chat pills and gallery open it; `FileViewerComponent` retired. |
| 3 | `feat/files-3-context-files` | §4: `ContextFilesForChat`, the endpoint, the Files tab, add-from-gallery for all file types, refresh on turn completion. |
| 4 | private `feat/files-4-c4a-sync` | §10: hydration on the public rule with hash compare, push on save/rename/save-as, gallery-save fan-out, persisted container mapping, `containerStatusForChat`. |
| 5 | `feat/files-5-editors-text` | Monaco code and Markdown editors (with preview), CSV as text, Save / Save As / Rename / conflict flow in the toolbar, phone `colorize` view. |
| 6 | `feat/files-6-editors-media` | Image editor, PDF annotations. |
| 7 | `feat/files-7-office-view` | Spreadsheet, document and slide **viewers**. |
| 8 | `feat/files-8-office-edit` | Spreadsheet editing (Univer + ExcelJS), document editing (ProseMirror round trip), slide text-in-place, fidelity warnings. |
| 9 | `feat/files-9-notebooks` | Notebook viewer and editor, nbformat read/write. |
| 10 | `feat/files-10-jupyterlite` | JupyterLite build, static hosting, the contents-drive extension, *Run*. |

Steps 5–10 are independent of each other once 2 is in; 4 depends on 3; everything else depends on 1 and 2.
Overlay files the private build shadows (`extensions/*`, `api-pricing.helpers`) are not touched by these steps, so no port is needed beyond the private spec for the Context tab if its inputs change.

## Later

- A push channel (SSE) so the Files tab and an open viewer update while the agent works, and a "the agent changed this file, reload?" banner in the editor.
- Version history and undo for saves.
- Collaborative cursors or presence in an open file are out of scope.
- Letting the agent propose edits to user files for approval (ADR 0x034 "Not done yet"), now with a diff viewer to show them in.
- Shape and media editing in slides; headers, footers and comments in documents.
- JupyterLite *Run* in the Capacitor app once service workers are confirmed there.
- A storage quota per account, now that users can create files.

## Decisions

All twelve confirmed by the user on 2026-11-06, with one change to §5: attachment pills open the viewer **and** keep a download action on the pill, rather than moving download into the viewer.

1. **Breakpoints:** large = ≥ 1280px (docked split), medium = 768–1279px (overlay, editable), phone = < 768px (view only). The app's current 1024px split stays for the context panel. Alternative: dock at ≥ 1024px and let the context panel auto-hide.
2. **Docked layout:** the workspace takes the right column; context-panel tabs open as a flyout over it. Alternative: a third column, with the panel auto-hidden when there is no room.
3. **Agent-written files of the chat join the in-context rule** (and so get hydrated into the container). This widens hydration slightly; agent files are text and ≤ 1 MB.
4. **A save moves every non-context-only row sharing the old object to the new one** ("the file is the object graph"). Alternative: only the edited row, accepting that chat copies become separate gallery files.
5. **No version history in v1.** Save As is the escape hatch.
6. **Monaco over CodeMirror**, with CodeMirror as the fallback if the WebView spike fails.
7. **Univer Sheets + ExcelJS** for spreadsheets (the only mature Apache/MIT grid with a formula engine; xlsx I/O done by us).
8. **Document editing via an HTML round trip with a lossy-save warning**, rather than view-only docx until a non-AGPL engine exists. Alternative: ship docx view only in v1 and evaluate EigenPal's MIT `docx-editor` later.
9. **Slides are text-in-place only** in v1.
10. **Notebooks: our own viewer/editor, JupyterLite only for Run**, web only at first.
11. **Save As from chat attaches via a system message** so it meets the existing rule, rather than a new chat-level files table. Alternative: add `chat_file_attachments` (chat-level, message-less) and teach the rule about it; cleaner, more schema.
12. **PR shape:** ten stacked steps as above; steps 5–10 can be parallelized across people.

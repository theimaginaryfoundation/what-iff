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
| document | `.docx` | Rendered to HTML with page layout | A rich-text editor over an HTML round trip, with a fidelity warning (see §7) | `docx-preview` (Apache-2.0) to view; `mammoth` (BSD-2) + ProseMirror/TipTap (MIT) + `docx` (MIT) to edit |
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
| **Save As** | `POST /image-gallery/import` (exists: multipart with `folder`) from the gallery; `POST /chat/{chatId}/file-attachment` (gains `folder`) from a chat | A new file in the gallery, filed where the user chose. From a chat it is a chat upload like any other: the workspace puts it in the composer's pending attachments, so it is attached to the next message and, in the private build, seeded into the live container. No new endpoint: step 1 found the two existing uploads cover it. |
| **Rename** | `PATCH /image-gallery/{id}` (exists) | Hardened: refuses a path clash in the folder (409), refuses skill-bundle files (403), normalizes the name, follows a changed extension with `file_type` when the allowlist knows it and the file stays an image or stays a non-image, and renames the chat copies sharing the object (the object-graph rule below). `updated_at` stays a content-change time and is not set by a rename. |

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
- `PUT /file-attachment/{id}/content`, `folder` on the chat upload, hardened rename, imports of non-images keyed under `users/{uid}/files/`; the gallery routes the web app already uses but `openapi.yaml` lacks (`GET /image-gallery`, `GET/PATCH/DELETE /image-gallery/{id}`, `/import`, `/reference`) are documented in the same change, since the SDK is generated from the spec.
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
- The initial bundle does not grow; each viewer is a lazy chunk. The heaviest (Monaco 0.7 MB gzipped plus its language workers, PDF.js ~1 MB, Univer 2.9 MB gzipped, JupyterLite ~tens of MB with Pyodide) load only when a file of that kind is opened, Univer only on *Edit*, and JupyterLite only on *Run*.
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


## Office suite (revision of 2026-11-07)

The office rows of §2, §7 and the delivery plan's steps 7 and 8 are replaced by this section.
The original plan settled for viewers plus lossy editors; the revised objective is a Docs / Sheets / Slides experience inside the workspace, with the office files as the storage format.
Three spikes on 2026-11-07 settled the bases (all Apache-2.0 or MIT):

| Format | Editor | Why | Converter |
|---|---|---|---|
| `.xlsx`, `.csv`, `.tsv` | **Univer Sheets** (`@univerjs/preset-sheets-core` plus data validation, conditional formatting, filter, sort, hyperlinks, drawings, find/replace, notes) | A real spreadsheet engine with formulas, styles, merges, freeze panes and a Google-Sheets-like UI; verified in step 0 | Ours, both ways, on ExcelJS: values, formulas and cached results, number formats, fonts, fills, borders, alignment, merges, column widths, row heights, hidden rows and columns, freeze panes, sheet order and tab colours, data validation, conditional formatting, hyperlinks, comments as notes, images. Charts, pivot tables and macros are not representable and are reported |
| `.docx` | **Univer Docs** (`@univerjs/preset-docs-core` plus hyperlinks and drawings) | Paginated, canvas-rendered editing with headers and footers, sections, lists, tables and images; its data model is designed around OOXML (named styles, numbering definitions, section breaks, footnotes, and *raw block* slots for lossless round-trip). The spike loaded a document from the model, rendered it on a page, edited it through the facade and read the snapshot back | Ours, both ways: `document.xml`, `styles.xml`, `numbering.xml`, headers, footers, media and relationships parsed with the browser's DOMParser and JSZip into `IDocumentData`, and written back from it. Block-level content the converter does not model (SmartArt, fields, content controls, equations) is kept as opaque raw XML in the model and re-emitted on export, so an edit elsewhere in the document never discards it |
| `.pptx` | **Our own Slides editor** | Univer Slides has a data model but no editor; nothing else open is close. The editor is DOM-based: each element is a positioned, rotatable box (text with rich runs, shapes as SVG paths, images, tables, lines), with selection, move, resize and rotate handles, slide thumbnails and reordering, speaker notes, undo and redo | Import with `pptxtojson` (MIT; the spike parsed text runs, bullets, shapes with their paths, tables with borders, images, backgrounds and notes), export with `pptxgenjs` (MIT). Animations, transitions, charts and SmartArt are not editable; they are reported, and charts are kept as pictures |

Rules that hold for all three:

- The editor is the viewer. One component per format, opened read-only in view mode (and always on a phone), so there is one rendering path and no second viewer to keep in step.
- Every converter returns the data **and a fidelity report**: what the file contains that the model does not keep. The report drives §7: the first save of a document with losses defaults to *Save As* and names what would be dropped; a document with no losses saves in place like any other file.
- Converters are pure TypeScript with their own tests on real files generated by `docx`, ExcelJS and `pptxgenjs`, plus round-trip checks (import, export, import again, compare).
- Libraries load on first use, per format. Univer Sheets is about 3 MB gzipped and Univer Docs about 2.3 MB; neither touches the initial bundle.

Delivery, replacing steps 7 and 8 (each a stacked PR on step 6):

| Step | Scope |
|---|---|
| 7 | The converters: `xlsx` (ExcelJS ↔ `IWorkbookData`), `docx` (OOXML ↔ `IDocumentData`), `pptx` (`pptxtojson` ↔ our deck model ↔ `pptxgenjs`), with fidelity reports and tests |
| 8 | The Sheets editor on Univer, as a `FileEditor`, with the save flow and the lossy-save warning |
| 9 | The Docs editor on Univer Docs, as a `FileEditor` |
| 10 | The Slides editor, as a `FileEditor` |

The notebook steps (9 and 10 in the original plan) move to 11 and 12.

### Steps 7–10 as built (2026-11-07)

All four steps landed on one branch, `feat/files-7-office-suite`, as four commits, under `web/app/src/app/features/office/`:

- `shared/`: the fidelity report and collector, A1 references, the OOXML package and XML helpers (JSZip plus the browser's DOMParser and XMLSerializer; twips, EMUs, half-points and eighths to and from 96-dpi pixels and points), and the dismissable fidelity notice every editor shows.
- `xlsx/`: ExcelJS ↔ Univer `IWorkbookData` as planned, plus CSV and TSV both ways. Styles are deduplicated into the workbook's style table. Plugin data (validation, conditional formatting, notes, images, the auto-filter) goes through Univer's `resources` slots. Defined names, charts, pivots and macros are reported as dropped. ExcelJS writes no cell that has neither a value nor a style, so a note on an empty cell gets an empty font to survive the round trip.
- `docx/`: our WordprocessingML ↔ `IDocumentData` layer. One finding changed the shape of it: Univer's renderer composes a run's text style from the document default, the paragraph's `textStyle` and the run, and does not look at `styles[styleId].textStyle`, while it does resolve paragraph properties through the named-style chain. The importer therefore bakes each named style's resolved text formatting into the paragraph's `textStyle` and the exporter subtracts it again, so Heading 1 renders as Heading 1 and exports as a style reference, not as direct formatting. Soft line breaks have no token in Univer's stream and become paragraphs (reported as a warning). Fields keep their last result and their instruction, so page numbers survive. Content with no counterpart is kept verbatim and written back at its position.
- `pptx/`: the deck model (slides of positioned elements in points; rich text as paragraphs of runs; fills, borders, crops, tables, charts with their series, groups), `pptxtojson` in and `pptxgenjs` out. Two pptxgenjs habits are rewritten after writing so strict readers (pptxtojson among them) accept the file: one paragraph-properties node per paragraph, and single-level chart categories as a plain string reference. Charts therefore round-trip as charts, not pictures.
- `univer/`: one lazy loader per suite (presets, English strings, and the preset stylesheets built as lazy global styles `univer-sheets.css` and `univer-docs.css`), and a shared editor base that owns the lifecycle: parse with the converter, create the Univer instance in the view, show the fidelity notice, mark dirty on content mutations (`CommandExecuted` with the mutation type, ignoring selection, scroll and zoom), hand the snapshot back through the converter on save, dispose.
- `sheets/` and `docs/`: the Sheets and Docs editors on that base. Read-only, the sheet keeps its tabs and zoom and drops the toolbar, formula bar and context menu; the document is locked through its permission facade.
- `slides/`: the Slides editor is ours end to end: a rail of thumbnails, the slide on a canvas laid out in CSS points and scaled to fit (a `ResizeObserver` keeps it fitted), shapes as SVG paths from a preset table or the imported path, text as HTML edited in place with `contenteditable` and read back by the same parser that reads pptxtojson's HTML, images with crops, tables edited per cell, charts drawn plainly from their data, groups nested. Selection, drag and resize use pointer events with eight handles; rotation is kept from the file but has no handle yet. Pure deck commands and a whole-deck undo history (slides are shared between versions) back the toolbar: text boxes, preset shapes, lines, pictures, bold/italic/underline, size, colour, alignment, bullets, fill and outline, arrangement, duplicate and delete, slide add/duplicate/move/delete, background, speaker notes, zoom.

Shared decisions:

- The registry marks these kinds `editInPlace`: the same component is the viewer and the editor, and the host flips its `readOnly` input rather than swapping components.
- `FileEditor` gained an optional `losses` signal. The host asks before saving over an original the editor cannot keep whole, naming what would be dropped and pointing at Save As.
- New preview kinds `spreadsheet`, `document` and `slides`; `.csv` and `.tsv` now open in the Sheets editor and save back as CSV.
- Specs fake the Univer kits through injection tokens; the slides specs run the real pptxgenjs and pptxtojson, which both work under the jsdom runner.
- A Playwright pass in Chromium against the branch's own API (mock LLM, throwaway Postgres) drove all three: open from the gallery, read-only view, Edit, a typed change, Save (`PUT …/content` 200), and the saved bytes read back with the change (ExcelJS for the sheet, `document.xml` for the document, pptxtojson for the deck, plus a reopen of the deck). It found three things the unit specs could not: Univer measured its container while the loading placeholder still hid it and drew a zero-height sheet; a host that does not constrain the height (the gallery page) gave it nothing to measure; slides inherited the app's dark theme as their text colour. All three are fixed on the branch.

### Fidelity pass against real documents (2026-11-07)

Three real files (a territory spreadsheet, a copay enrolment fax, a pitch deck) were opened side by side with Excel, Word and PowerPoint, and the differences fixed:

- **Theme colours.** Most fills and font colours in real workbooks are theme references with a tint, not literal RGB; they came through as nothing. The importer now reads `theme1.xml` and resolves theme indices (with the dk1/lt1 pair swap SpreadsheetML uses), tints in HSL, and the legacy indexed palette. Conditional-formatting colours go the same way.
- **Charts and pivot tables.** ExcelJS reads and writes neither. The importer collects the original chart parts (with their drawing anchors, style parts and media) and pivot parts (tables and caches) from the package, draws each chart from its cached series at its anchor through a shared SVG renderer, and the exporter writes the parts back under names ExcelJS never uses, re-linking sheet, drawing and workbook relationships and content types. The notice says charts and pivots are kept but not editable here; Excel refreshes pivots from their cache. A sheet whose drawing holds a chart gets its original drawing back whole, so pictures on that sheet are saved as they were.
- **Office fonts.** Aptos, Calibri, Cambria, Segoe UI, Poppins and the rest alias to local lookalikes through `@font-face` `local()` rules (`src/styles/office-fonts.scss`), so canvas and DOM text stops falling back to the browser serif. Nothing is fetched.
- **Docs.** Word wraps cells and rows in content controls; the table walker now looks through them (the fax form's table had lost half its cells). Pictures reserved their space but were never painted: Univer's layout reads the document-level `drawings` map, but the docs-drawing plugin that paints them loads from its resource entry (`DOC_DRAWING_PLUGIN`, `{data, order}` as JSON), so imported documents carry both; header and footer pictures live in the document-level map too, with the per-page transform Univer expects. Drawings inside `mc:AlternateContent` are read. A read-only document is a disabled snapshot rather than a permission lock, which keeps Univer's "content is protected" toast out of the viewer.
- **Read-only, enforced twice.** Univer's workbook lock did not hold on a freshly created unit (its permission points register as it comes up), and a reader could type into a cell. The editor layer now stops keys that would type, delete or paste and double-clicks before they reach Univer, and sets the sheet lock again once the unit is up.
- **A developer harness.** `features/office/real-files.spec.ts` runs the converters over the files in `OFFICE_SAMPLES_DIR` and writes JSON reports beside them; it is skipped otherwise. Fidelity work is checked against real documents with it, then in Chromium against the composed dev stack.
- **Slides.** Run colours and sizes never reached the canvas: Angular's sanitiser strips inline styles from `[innerHTML]`, so the model-built HTML (escaped text, styles from known fields) is marked trusted. Picture and gradient fills render on shapes. The slide fits its stage from the moment the deck shows (the stage only exists once there is a deck), a status bar carries paging and zoom in both modes, and a rotation handle rotates elements about their centre.
- **Saving across origins.** The dev app on :4200 saving to the API on :8080 failed the CORS preflight: `If-Match` was not an allowed request header and `ETag` not exposed. Fixed in the server's CORS policy; the earlier harness had fulfilled CORS itself and hidden it.
- **Monaco** gets light and dark themes on the app palette, following `<html data-theme>`, and the code editor the same minimum height as the office editors on the gallery page.
- **Start-up mutations.** A Univer unit runs mutations of its own as it settles; the editor marks dirty only after that.


### Preservation pass (2026-11-07)

The rule for v1: saving a file here must not lose what the editor does not model. Each format now keeps the original package and changes only what the edit touched, which supersedes the converter descriptions above where they differ.

- **`.pptx` saves by patching, not rebuilding.** Slides and shapes carry their source identity (`sourcePath`, `sourceId` from `p:cNvPr@id`). Every untouched part is copied byte for byte, so masters, layouts, theme, media, charts, animations, comments, sections and fonts survive. On a changed slide only the changed shapes are patched, using the original paragraph and run properties as templates. Slides added, copied, removed or reordered update the slide list, relationships, content types and sections. pptxgenjs remains only for a deck with no original. pptxtojson lists slides by part number rather than `p:sldIdLst`, so the importer now orders them itself.
- **`.docx` saves by patching.** Only `document.xml`, the modelled headers and footers, numbering additions and note stories are regenerated. Styles, settings, theme, font table, comments, glossary, customXml and docProps stay byte-identical, and original style and numbering ids are kept. Text boxes, shapes, charts, objects and equations ride as opaque runs at their position (`docxRawCustomBlocks`). Footnotes and endnotes are native Univer notes (the renderer reads `snapshot.notes`, not `footnotes`). Bookmarks and comment anchors are ranges that move with edits.
- **`.xlsx` keeps its package parts beside ExcelJS.** Because ExcelJS rewrites the workbook, what it cannot carry is reinjected: charts, pivots, tables, external links, VBA (with the macro-enabled content type and MIME), customXml, custom properties, printer settings, sparklines and drawings holding shapes. Page setup, headers and footers, views, protection, outline levels, workbook and document properties and the original theme pass through ExcelJS. Defined names, including formula-valued and sheet-scoped ones, are editable through Univer's `SHEET_DEFINED_NAME_PLUGIN` resource and written back to `workbook.xml`. Shared formulas keep their translated text, array formulas their `ref`. Every save sets `fullCalcOnLoad`, so Excel recomputes on open.
- **Formulas run in a worker.** `univer-formula.worker.ts` runs `UniverSheetsCoreWorkerPreset`; the sheets editor creates one worker per instance, passes it to the preset as `workerURL`, and terminates it on dispose. Where workers are unavailable the engine stays on the main thread.
- **Notices stay honest.** Anything kept but not editable is a warning that says so; only what is truly lost is a drop.

Still lost or approximate: slicers (dropped, with a notice); threaded comments become notes; content-control properties and tracked changes (content kept, changes accepted); soft line breaks in documents become paragraphs; x14 conditional formatting and validation in `extLst` are not read; pictures moved on a sheet whose drawing is kept whole are saved where they were; charts and pivots stay view-only.

Found by the browser pass:

- **Slide text was wiped while typing.** `textHtml()` returned a new `SafeHtml` on each check, so Angular re-set `innerHTML` on the element being edited. It is now cached per text body, which the model replaces rather than mutates.
- **Viewers painted through the context panel.** Univer's canvases carry high z-indexes and the docked panel made no stacking context. The docked panel and the Univer stage now use `isolation: isolate`.
- **A reopened file showed its old bytes.** The download was cached for an hour under a URL whose bytes a save replaces, so a reload after saving showed the previous version. Gallery images had the same problem. Both routes now revalidate: `private, no-cache` with `content_version` as the ETag, and a 304 on a match. Earlier passes missed this because their harness forwarded requests outside the browser cache.
- **A long deck pushed the slide down the gallery page.** The rail stretched the stage to its own height; it now scrolls inside the stage.
- **The context panel covered an opening file.** Opening a file in a thread now hides it without changing the remembered open or closed choice; the rail brings it back over the file.

Verification: the converters over the three real files and a fixture with a text box, footnote, bookmark, internal link and comment; the same files edited and saved in Chromium, with the saved bytes compared part by part; independent reads with openpyxl, python-docx, python-pptx and LibreOffice. Microsoft Office itself has not opened these files yet.

### Step 11 as built: notebook viewer and editor (2026-11-07)

`features/notebook/`, as §8 planned, one component that is both viewer and editor (`editInPlace`):

- **Format.** `nbformat.ts` reads and writes nbformat 4 by hand and keeps what it does not model in `raw`, so metadata, attachments and unknown fields survive, and an untouched notebook writes back byte for byte. Formats before 4 are refused with a reason.
- **Cells.** At rest a cell is rendered Markdown (the chat's renderer with KaTeX, `attachment:` images resolved) or colourized code. Only the cell being edited holds a Monaco editor, and off-screen cells skip layout through `content-visibility`, so long notebooks stay responsive. Structural edits are pure commands with whole-version undo; a run of typing in one cell is one step. Jupyter's command-mode keys work (`a`, `b`, `dd`, `m`, `y`, arrows, Enter, Escape, Shift+Enter).
- **Outputs are untrusted.** Picked by MIME type from the richest safe representation. Scripts never run in the page (see step 12 for the sandboxed frames). HTML goes through a dedicated DOMPurify instance that removes scripts, handlers, forms, frames and stylesheets, and only embedded pictures load, so opening a notebook cannot phone home. SVG shows through an `<img>`, where it cannot script. Output is `contain`ed, so a `position: fixed` element cannot cover the app. Scripted HTML steps aside for a picture or text fallback; widgets get a note.
- **Not done:** formats before 4.

### Step 12 as built: Run with JupyterLite (2026-11-07)

Two decisions differ from §8:

- **JupyterLite gets its own origin, not `/jupyterlite/`.** A same-origin iframe shares `sessionStorage` with its parent, so a notebook's code or output could read the session token. `usableRunUrl` refuses a same-origin URL, so Run stays off rather than running unsafely. The site holds no credentials and receives only the notebook.
- **A bridge, not a custom contents drive.** A small labextension (`web/jupyterlite/extension`) writes the host's notebook to JupyterLite's in-memory default drive and opens it, so a kernel reads it and its neighbours with plain `open()`. The extension reports saves back, and the host persists them through its usual `PUT …/content`.

How it fits together:

- **Entering Run.** Run is in the notebook toolbar in edit mode, so phones do not get it. JupyterLite takes the cells' place. Back to cells asks it to save first.
- **Saving.** A save inside JupyterLite becomes the notebook's new state and goes through the host's normal save, and undo can step back over it.
- **Trust at the boundary.** The host accepts messages only from its own frame on the configured origin. The extension accepts them only from its parent on an origin in `whatiffHostOrigins`.
- **Build and configuration.** `make jupyterlite` builds the 65 MB site with versions pinned, and `make jupyterlite-serve` serves it on :4203 for development. `environment.jupyterliteUrl` (`JUPYTERLITE_URL` at image build) points at it; empty leaves Run off.

Hosting requirements for production:

- A static host on its own origin, such as a separate subdomain; it needs nothing from the API.
- The app's origin in `JUPYTERLITE_HOST_ORIGINS` at build time.
- `frame-ancestors` limited to the app.
- A correct WASM MIME type.

Live widgets work in Run: `jupyterlab_widgets` is in the site build, and a slider drove Python and back in Chromium. Plain `import numpy`, `pandas`, `matplotlib`, `ipywidgets` and `plotly` install themselves. `importlib.import_module` does not trigger that, because Pyodide works from the `import` statements in the source.

Script-driven output in the viewer (a decision of 2026-11-07, which relaxes step 11's "scripts never run"):

- **Asked for, per notebook.** Nothing runs on open; the reader sees the output's static fallback and a button. The choice lasts until another file opens and is not stored.
- **Contained.** Each output runs in its own frame with `sandbox="allow-scripts"` and nothing else, so it has an opaque origin. In Chromium a hostile output could not reach the token, storage, cookies or the parent page, open a popup or navigate the app. A frame's policy limits it to https and blocks forms and `<base>`. The host listens to a frame only for its height, identified by its window.
- **The trade-off, accepted.** After the click, output can load libraries from CDNs and could send the data it was saved with to any https host. The notebook's frames hold only that notebook's data.
- **What draws.** Plotly and Vega-Lite or Vega from their own MIME data, with pinned libraries from jsDelivr; other scripted HTML as it is. Outputs that only load a library, such as Plotly's `init_notebook_mode` or Bokeh's `output_notebook`, show nothing and are included in each frame. Plotly, Vega-Lite and a loader-dependent output were checked in Chromium; real Bokeh and Altair output was not.
- **Widgets** need a kernel, so outside Run they show a note pointing there.

Production hosts: `notebooks.whatiff.chat` and `notebooks-dev.whatiff.chat`, each an S3 bucket behind CloudFront, deployed from the private repo.
Python loads from the jsDelivr CDN at run time, the kernel's default, which was accepted on 2026-11-07 rather than self-hosting hundreds of megabytes.

Verified in Chromium against the composed stack: the frame could not read the app's token, Python ran, read its own notebook with `open()`, a save reached `PUT …/content`, and the output showed in our cells.

## Step 0 results (2026-11-06)

The four spikes ran on `spike/file-workspace` (throwaway; scripts under `web/app/spike-scripts/`, the Monaco route at `/spike/monaco`), in headless Chromium.
Nothing here changes the decisions; the recipes and numbers below are what steps 2, 5, 6 and 8 build on.

### Monaco 0.57 under the esbuild application builder: works

- **Recipe.** Workers are local entry files (`src/.../workers/json.worker.ts` containing `import 'monaco-editor/language/json/json.worker'`; the package's exports map rewrites `monaco-editor/<p>` to `esm/vs/<p>.js`), referenced as `new Worker(new URL('./workers/json.worker', import.meta.url), { type: 'module' })`; `angular.json` gains `webWorkerTsConfig` (a `tsconfig.worker.json` with `noUncheckedSideEffectImports: false` for TypeScript 6) and `loader: { ".ttf": "file" }` for the codicon font. Monaco's stylesheet is a lazy global style (`styles` entry with `inject: false`, `bundleName: "monaco"`) that the loader links before the first editor, because CSS imported from TypeScript is emitted by esbuild but never loaded by the page. Language tokenizers are registered lazily by Monaco itself, so importing `monaco-editor` once costs nothing per language; each tokenizer is its own chunk.
- **API note.** 0.57 moved the TypeScript service from `monaco.languages.typescript` to `monaco.typescript`.
- **Verified.** JSON and TypeScript diagnostics from their workers, TypeScript completions and the suggest widget, Python tokenization, `editor.colorize` to static HTML for the phone view, codicon icons, no console errors. The initial bundle is unchanged (1.38 MB).
- **Sizes** (raw / gzipped): editor chunk 2.76 MB / 717 kB; stylesheet 391 kB / 127 kB; workers: editor 307 kB / 95 kB, JSON 437 kB / 133 kB, CSS 1.08 MB / 251 kB, HTML 755 kB / 200 kB, TypeScript 7.0 MB / 1.56 MB. The TypeScript worker loads only for `.ts`/`.js` files.
- **As the VFS editor** (`/spike/vfs?file=<id>`, mocked API). A gallery file opens through the existing `ImageGalleryService.getFileInfo` and `FileAttachmentService.downloadFileAttachment`; the language comes from Monaco's registered extensions for the file name (`notes.py` → python, `config.json` → json, else plain text); the model's URI is `vfs:///<folder>/<name>` so the language services key on the path; edits mark the file dirty; Ctrl+S sends `PUT /file-attachment/{id}/content` with the body, the file's MIME type and the loaded `content_version` as `If-Match`, and adopts the version the server returns; a stale version (`412`) surfaces as a conflict instead of overwriting. The JSON worker flagged duplicate keys in a VFS file. This is the shape of the step 5 editor; nothing in it touches a file on the host.
- **`ngx-monaco-editor-v2`** (MIT, Angular 22) was considered as the wrapper. It is a thin component around the same Monaco, loaded through the AMD `min/vs` build copied into `assets/` (25 MB, 151 files, every language and worker shipped whether used or not) rather than through esbuild. The ESM route above keeps per-language chunks and the workers in the normal build pipeline, so step 5 writes its own small component on the loader described here; the wrapper remains a fallback if the ESM integration gives trouble in a later Angular upgrade.
- **Open.** The Capacitor WebView (the private build's Android app shell under `mobile/`, which runs this same bundle for the tablet editing case) was not exercised (no Android SDK on the build machine); module workers need Android System WebView 80 or newer, which Play keeps current on API 23+ devices. Step 2 checks it on a device before Monaco ships.

### PDF.js 6.5 annotation save: real annotations

- A FreeText note and an Ink stroke written through `annotationStorage` and `saveDocument()` come back as `/FreeText` and `/Ink` annotation objects in the saved file, read by pdf-lib (an independent parser) and by PDF.js on reload, and render. The claim that PDF.js editor annotations do not survive into the file is wrong for these types. Highlight and Stamp, and the viewer's editor UI layer (`web/pdf_viewer.mjs`), were not exercised; step 6 does that.

### Univer 1.3.1 Sheets with ExcelJS: round trip holds for the §7 list

- Packages are `@univerjs/presets` plus `@univerjs/preset-sheets-core` (Apache-2.0). A 90-line mapping between ExcelJS and Univer's `IWorkbookData` carried values, formulas (with cached results), number formats, bold and italic, fills, merges, column widths, dates and booleans through load, edit and save; Univer computed the formulas (including a cross-sheet one) and recomputed after an edit. Data validation was dropped, as §7 says.
- **Size.** 12.0 MB raw / 2.9 MB gzipped plus 120 kB of CSS: the heaviest library in the plan. It loads only when the user enters edit mode; the spreadsheet *viewer* stays on ExcelJS to HTML (step 7).

### docx-preview 0.4.1 and mammoth 1.13: viewer is faithful, the round trip is lossy as expected

- `docx-preview` (Apache-2.0, 175 kB bundled) rendered headers, footers, footnotes, a page break into two pages, an image, a table with a merged cell, a hyperlink, a custom paragraph style, colour, underline and alignment with no errors.
- `mammoth`'s HTML kept headings, bold, italic, lists, the table with its colspan, the image, the link and the footnote, and dropped the header and footer, colour, underline, font size, alignment and the custom style (it warns about the style). That is the loss the editor's first-save warning names, and why decision 8 defaults a lossy document to *Save As*.

## Later

- A push channel (SSE) so the Files tab and an open viewer update while the agent works, and a "the agent changed this file, reload?" banner in the editor.
- Version history and undo for saves.
- Collaborative cursors or presence in an open file are out of scope.
- Letting the agent propose edits to user files for approval (ADR 0x034 "Not done yet"), now with a diff viewer to show them in.
- Shape and media editing in slides; headers, footers and comments in documents.
- Office limitations parked after v1 (2026-11-07; see "Preservation pass"), each a candidate for a later pass:
  - Sheets: slicers are dropped, with a notice; threaded comments become notes; x14 conditional formatting and validation in `extLst` are not read; pictures moved on a sheet whose drawing is kept whole are saved where they were; charts and pivots are view-only; editing defined names in Univer's UI has not been exercised.
  - Docs: tracked changes are accepted on import; content-control properties are lost, though their content stays; soft line breaks become paragraphs; text boxes, shapes, charts and equations are kept but not shown; a heading applied in the editor does not export as a paragraph style.
  - Slides: gradient and picture fills, group contents, table geometry and video or audio are kept but not editable.
  - All formats: saved files have not yet been opened in Microsoft Office. LibreOffice and independent parsers read them.
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
11. **Save As from chat attaches via a system message** so it meets the existing rule, rather than a new chat-level files table. Alternative: add `chat_file_attachments` (chat-level, message-less) and teach the rule about it; cleaner, more schema. *Resolved in step 3:* no system message. The file is created as a chat upload (`POST /chat/{chatId}/file-attachment` with `folder`) and lands in the composer's pending attachments, so it is attached to the next message; the Files tab lists it under "With your next message" until then, so it is visible at once without bending the rule.
12. **PR shape:** ten stacked steps as above; steps 5–10 can be parallelized across people.

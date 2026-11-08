# ADR 0x043: The file workspace: viewing and editing files inside the app

- **Status:** Accepted and implemented
- **Date:** 2026-11-07
- **Deciders:** What Iff maintainers
- **Builds on:** the gallery as a virtual file system and agent file tools ([ADR 0x034](0x034-agent-files-and-entity-wiki.md)), and skill bundles ([ADR 0x035](0x035-agent-skills.md)).

## Context

Working with files in a conversation used to be a round trip.
A user uploaded a file, the agent read or transformed it, the result came back as an attachment, and the user downloaded it, opened it somewhere else, changed it, and uploaded it again.
Nothing in the app let the user see what the agent was working with, or change a file in place and have the agent see the change.

The app had a read-only gallery viewer, attachment pills that only downloaded, and no way for a user to overwrite a file's bytes through the API.
"The files in this conversation" was not a stored set, a function or an endpoint.
There was no server push channel, no web workers, no syntax highlighter and no tablet layout.
The web app is Angular 22 on esbuild with a 2.5 MB initial-bundle budget, and the repository is Apache-2.0.

The goal is for a conversation to feel like working *with* the agent on a shared set of files: see what it sees, open any of it in place, change it, and have the change reach the agent without leaving the thread.

## Decision

### 1. One file workspace, hosted in two places

A single **file workspace** owns which file is open, in which mode and in which layout.
The chat page hosts it beside the thread and the gallery hosts it in place of a separate viewer.
`?file=<id>` on either route opens it, so the URL is what is open: a link opens a file and Back closes it.
The same components render everywhere, so a file behaves the same whether it was opened from a message, the Files tab or a gallery tile.

| Viewport | Layout | Editing |
|---|---|---|
| **Large** (≥ 1280px) | Docked beside the thread, resizable from 40% to 75% of the viewport, with an expand to full screen. The context panel opens as a 320px flyout over it. | Yes |
| **Medium** (768–1279px) | Full-size overlay over the chat, through a `ui-overlay` primitive with a focus trap, Escape and scroll lock. | Yes |
| **Phone** (< 768px) | Full screen, replacing the thread. | **View only** |

The 1024–1279px band behaves as medium because it is too narrow for a thread, a panel and an editor.
The open file and the layout are remembered per chat in local storage.
Opening a file hides the context panel without changing the remembered open or closed choice; the rail brings it back over the file.
The docked panel and the Univer stage use `isolation: isolate`, because Univer's canvases carry z-indexes that would otherwise paint through the flyout.

### 2. Viewers and editors are a registry of lazily loaded components

`features/file-workspace/viewers/registry.ts` maps each preview kind to a viewer, an optional editor, and whether the two are one component (`editInPlace`).
Every heavy library sits behind that kind's `import()` and never enters the initial bundle.
Editors implement `FileEditor`: `dirty`, `content()`, `markSaved()`, and an optional `losses` that makes the host confirm before saving over an original the editor cannot keep whole.

| Kind | Extensions | View and edit | Library (license) |
|---|---|---|---|
| text | code and text files | Monaco editor; `colorize` to static HTML in the viewer | `monaco-editor` (MIT) |
| markdown | `.md .markdown .mdx` | Rendered with a raw toggle; Monaco with a live preview | `monaco-editor`, `ngx-markdown` |
| image | png, jpeg, gif, webp | Crop, rotate, resize, annotate, filters | `filerobot-image-editor` (MIT) |
| pdf | `.pdf` | Pages, text, search, zoom; annotations written into the file | `pdfjs-dist` (Apache-2.0) |
| spreadsheet, csv | `.xlsx .xlsm .csv .tsv` | One component, section 8 | Univer Sheets (Apache-2.0), ExcelJS (MIT) |
| document | `.docx` | One component, section 9 | Univer Docs (Apache-2.0) |
| slides | `.pptx` | One component, section 10 | our own editor, `pptxtojson` (MIT), `pptxgenjs` (MIT) |
| notebook | `.ipynb` | One component, section 11 | our own cells, JupyterLite (BSD-3) to run |
| none | archives, unknown binaries | Badge and download | |

GPL, AGPL and commercially licensed dependencies are not allowed; SuperDoc, OnlyOffice, Handsontable and Pintura were ruled out on that basis.
This rests on review: no automated licence check exists.

**Monaco under esbuild.** Workers are local entry files such as `workers/json.worker.ts`, referenced with `new Worker(new URL(...), { type: 'module' })`.
`angular.json` has `webWorkerTsConfig` and a `.ttf` file loader for the codicon font.
Stylesheets that load on demand (Monaco, PDF.js, Univer) are lazy global styles with `inject: false`, which a loader links on first use, because CSS imported from TypeScript is emitted by esbuild but never loaded by the page.

### 3. Save, Save As, Rename, and what "the same file" means

`file_attachments` has `content_version`, bumped by every content write, and `content_sha256`.
A viewer carries the version it loaded and every save sends it back.

| Action | API | Semantics |
|---|---|---|
| **Save** | `PUT /file-attachment/{id}/content` with `If-Match: <content_version>` | Uploads a new object and swaps the key conditionally. A mismatch is `412`, and the editor offers Reload, Overwrite or Save As. |
| **Save As** | `POST /image-gallery/import` from the gallery; `POST /chat/{chatId}/file-attachment` with `folder` from a chat | A new file where the user chose. From a chat it joins the composer's pending attachments, so it is attached to the next message. |
| **Rename** | `PATCH /image-gallery/{id}` | Refuses a path clash (409) and skill-bundle files (403), normalizes the name, follows a changed extension with `file_type`, and renames chat copies sharing the object. |

"The same file" is the **object graph**, not the row.
A message attachment is often a reference row sharing `s3_key` with the gallery original.
A save moves every row that shares the old key and is not `context_only` to the new key in one update, so a gallery file and its chat copies stay one file.
`context_only` rows keep the old object because they record what a reply saw; it is released when nothing references it.

Saving a chunked upload deletes its chunks and re-runs the pipeline, so `find_context` serves the new text.
Saving an image regenerates its thumbnail.

Downloads revalidate: `GET /file-attachment/{id}` and `GET /image-gallery/{id}` send `Cache-Control: private, no-cache` with `content_version` as the ETag, and answer a matching `If-None-Match` with 304 before touching the store.
A save replaces bytes under the same URL, so a time-based cache would show the old version after a reload.

`internal/filechange` raises an event after every content write, rename and create, for anything that needs to follow a file.
The user owns everything and may edit any of their files, including agent-authored ones; agents still edit only what they created ([ADR 0x034](0x034-agent-files-and-entity-wiki.md) §3).
There is no version history: Save overwrites, and Save As keeps a copy.

### 4. "Files in context" is a rule with an endpoint, and the Files tab shows it

`internal/agent/contextfiles.go` defines `ContextFilesForChat`: attachments on messages since the checkpoint, `find_context` results in those messages, the last 10 user messages' attachments, this turn's tool outputs, active skill bundles, and agent-authored files created in this chat.
Each file has a workspace path, and the reasons it is there.
Deduplication is newest wins per path; shadowed duplicates are listed greyed out, so the name-collision rule is visible.

`GET /chat/{chatId}/context-files` returns the list.
The response has an optional `container` field for deployments that run agents in containers; it is absent otherwise.

The **Files** tab is a context-panel tab.
A row shows the name, type badge and why the file is there, and opens the workspace.
Upload and From gallery add a file to the next message, and pending files are listed under "With your next message".
It refreshes when a turn completes, when the app regains focus and after a save.

### 5. Chat and gallery

Attachment pills and image cards open the workspace, so a file the agent just produced is one tap from the message that shows it.
Pills keep a download button.
The gallery's rename, move, delete, previous and next actions live in the workspace toolbar, and its import accepts any allowed file type.

### 6. Fidelity: saving never loses what the editor does not model

**Each format keeps the original package and changes only what the edit touched.**
A file nobody edited stays byte-identical part by part, or equivalent where a library must rewrite it, and anything kept but not editable is named in a notice.
Every converter returns its data and a fidelity report: `dropped` for real losses, which make the host confirm before saving, and `warnings` for what is kept or approximated.

The editor is the viewer: one component per office format, opened read-only in view mode and always on a phone.
Read-only is enforced twice, because Univer's workbook lock does not hold on a freshly created unit: the editor layer stops keys and double-clicks that would change content, and sets the lock again once the unit is up.
A unit runs mutations of its own as it settles, so the editor marks dirty only after that.

Office font names (Aptos, Calibri, Cambria, Segoe UI and others) alias to local lookalikes through `@font-face` `local()` rules, and nothing is fetched.
Each alias declares regular, bold and italic faces by full font name, because with a single face bold text was left to the browser's synthesis and showed as regular.

### 7. The Univer editors

`features/office/univer` holds one lazy loader per suite and an editor base that owns the lifecycle: parse with the converter, create the instance, show the notice, mark dirty, serialize, dispose.
Univer needs a visible container with a definite height when created, so the stage has one and the placeholder sits over it.

Formulas run in `univer-formula.worker.ts`, one worker per sheets instance, terminated with it.
Where workers are unavailable the engine stays on the main thread.

All Univer packages must be the same version: a mismatched preset loads a second copy of its core and breaks dependency injection, so `@univerjs/preset-sheets-thread-comment` is pinned.

### 8. Spreadsheets

`features/office/xlsx` converts between ExcelJS and Univer's `IWorkbookData`; `features/office/sheets` is the editor.
Saving writes through ExcelJS, then reinjects what it cannot carry from the original package.

| Feature | Behaviour |
|---|---|
| Cells, styles, layout | Values, formulas with cached results, shared and array formulas, number formats, fonts, fills, borders, alignment, merges, sizes, freeze panes, tab colours, row and column styles and outlines, images, notes, hyperlinks, the auto-filter. Theme and indexed colours and tints are resolved on read. |
| Carried through | Page setup, headers and footers, views, protection, workbook and document properties, the original theme. Every save sets `fullCalcOnLoad`. |
| Kept in the package | Tables, external links, VBA (with the macro-enabled content type and MIME), customXml, custom properties, printer settings, sparklines, and shapes, text boxes and SmartArt in drawings. |
| Defined names | Editable through Univer's resource, including formula-valued and sheet-scoped names, and written to `workbook.xml`. |
| Conditional formatting, validation | Read and written directly from the sheet XML rather than through ExcelJS, in priority order, including x14 rules: data bars with their extras, icon sets, cross-sheet rules and lists. |
| Threaded comments | Threads, replies, resolved state, people and mentions go to Univer's comment model and back to their parts, with the legacy note Excel expects. |
| Moved pictures | A save patches anchors in a kept drawing. |

The chart, pivot and slicer panels read the sheet through `SheetGrid`, a small interface over Univer's facade, so their logic is tested against an in-memory grid.

**Charts.** Univer's open-source packages have no charts, so ours are pictures Univer can move and resize, drawn by `shared/chart-svg.ts` from a chart model parsed out of the chart XML.
They redraw from live cells, and leave out hidden rows when the chart does.
The panel edits type, grouping, title, legend, axis titles, series references and colours, and adds and deletes charts.
An untouched chart stays byte-identical, a chart whose cells changed gets new caches only, and an edit patches the original XML, keeping its style parts, formatting and `extLst`.
Univer's image update does not repaint a changed source, so the editor calls the shape's `changeSource` directly.

**Pivot tables.** `sheets/pivots/pivot-engine.ts` groups and aggregates from the source range in the compact layout.
The panel assigns fields to Rows, Columns, Values and Filters, with 11 aggregations, item filters, totals, refresh and new from selection.
An untouched pivot stays byte-identical. An edited one regenerates its layout XML and sets `refreshOnLoad`, so Excel rebuilds it on open.

**Slicers.** Pivot-backed and table-backed slicers are read, kept and written back, and filter from the panel.
A pivot slicer recomputes every pivot it drives; a table slicer hides rows and writes the table's `filterColumn`.
They draw as pictures at their anchors. Univer's float DOM needs a React component, so they are not clickable on the grid.

### 9. Documents

`features/office/docx` is our own WordprocessingML to `IDocumentData` layer, because Univer has no open-source OOXML import or export.
Saving regenerates `document.xml`, the headers and footers, numbering additions and note stories, and leaves styles, settings, theme, fonts, comments, glossary, customXml and docProps byte-identical, with original style and numbering ids kept.

Univer's renderer composes a run's style from the document default, the paragraph's `textStyle` and the run, and does not look at `styles[styleId].textStyle`.
The importer therefore bakes each named style's text formatting into the paragraph and the exporter subtracts it, so Heading 1 renders as Heading 1 and exports as a style reference.
The page shows what prints. **Show guides**, in edit mode, draws the margin corners and dashed lines for table sides with no border. It only changes painting, so layout and the file are untouched, and it reaches two fields of the pinned Univer version because there is no option for either.
Pictures are in both `drawings`, which layout reads, and the `DOC_DRAWING_PLUGIN` resource, which the painter loads.
A read-only document is a disabled snapshot rather than a permission lock, which keeps the "content is protected" toast out of the viewer.

| Feature | Behaviour |
|---|---|
| Table borders | Resolved as Word does: the table style and its `basedOn`, the table's `tblBorders`, first and last row and column formatting where `tblLook` turns it on, then the cell's `tcBorders`. A side with no line is stated as invisible, because Univer draws a default gray line where a cell has no border data, and saves as `nil`. |
| Soft line breaks | Stay inside their paragraph as an unmarked `\v`, which Univer draws as a line break in a paginated document; column breaks are marked as such. |
| Content controls | `CustomRangeType.SDT` ranges at run, block, row and cell level, with `w:sdtPr` kept. Typing over a placeholder clears `w:showingPlcHdr`. Univer enforces `w:lock`. |
| Tracked changes | Insertions stay wrapped and show coloured on screen, with nothing added to the saved formatting. Deletions, moves and property changes ride through as they were. |
| Footnotes, endnotes | Native Univer notes, in `snapshot.notes`. |
| Bookmarks, comments | Ranges that move with edits; `comments.xml` is untouched. |
| Text boxes, shapes, charts, objects, equations | Opaque runs at their position (`docxRawCustomBlocks`), kept but not shown. |

### 10. Slides

`features/office/slides` is our own DOM-based editor, because Univer has no slide editor: a rail of thumbnails, a canvas in points that scales to fit, shapes as SVG, rich text edited in place, images, tables, charts and groups.
Selection, drag, resize and rotate use pointer events, and pure deck commands back whole-deck undo.

Saving patches the original package.
Slides and shapes carry their source identity (the part path and `p:cNvPr@id`), so untouched parts are copied byte for byte, keeping masters, layouts, theme, media, charts, animations, comments, sections and fonts.
A changed slide has only its changed shapes patched, using the original paragraph and run properties as templates.
Slides added, copied, removed or reordered update the slide list, relationships, content types and sections.
`pptxgenjs` writes a deck only when it has no original.

`pptxtojson` lists slides by part number, so the importer orders them by `p:sldIdLst` itself.
The canvas caches trusted HTML per text body, because a new `SafeHtml` on each check makes Angular set `innerHTML` again, wiping what is being typed.

### 11. Notebooks

`features/notebook` is our own viewer and editor.
`nbformat.ts` reads and writes nbformat 4 by hand and keeps what it does not model, so metadata, attachments and unknown fields survive and an untouched notebook writes back byte for byte.
At rest a cell is rendered Markdown or colourized code. Only the cell being edited holds a Monaco editor, and off-screen cells skip layout through `content-visibility`, so long notebooks stay responsive.
Structural edits are pure commands with undo, and Jupyter's command-mode keys work.

**Outputs are untrusted.**
HTML goes through a dedicated DOMPurify instance that removes scripts, handlers, forms, frames and stylesheets, and only embedded pictures load.
Output is `contain`ed, so a `position: fixed` element cannot cover the app, and SVG shows through an `<img>`, where it cannot script.

**Script-driven output** (Plotly, Vega, Bokeh, maps) runs only after the reader asks, per notebook, until another file opens.
Each output has its own frame with `sandbox="allow-scripts"` and nothing else, so it has an opaque origin and cannot reach the page, its storage or its cookies, open a popup or navigate the app.
A policy limits the frame to https and blocks forms and `<base>`, and the host listens to it only for its height.
Plotly and Vega draw from their own MIME data with pinned libraries from jsDelivr.
Outputs that only load a library show nothing and are included in each frame.
After the click, output can load from CDNs and could send its own saved data to an https host; that trade-off is accepted for the value of seeing these plots, and the click keeps opening a notebook silent.
Widgets need a kernel, so outside Run they show a note pointing there.

**Run** executes in the browser with JupyterLite and Pyodide; nothing runs on our servers.
It is in the notebook toolbar in edit mode, so phones do not get it, and JupyterLite takes the cells' place until Back to cells, which asks it to save first.

- **Its own origin.** A same-origin iframe shares `sessionStorage` with its parent, so a notebook's code or output could read the session token. `usableRunUrl` refuses a same-origin URL and Run stays off. `environment.jupyterliteUrl` (`JUPYTERLITE_URL` at image build) points at it; empty leaves Run off.
- **A bridge.** A small labextension in `web/jupyterlite/extension` writes the host's notebook to JupyterLite's default drive, kept in memory so nothing carries over to the next notebook or account, and opens it, so a kernel reads it with plain `open()`. Saves come back over `postMessage` and go through the usual `PUT …/content`, and undo can step over them.
- **Trust at the boundary.** The host accepts messages only from its own frame on the configured origin, and the extension only from its parent on an origin in `whatiffHostOrigins`.
- **Packages.** `jupyterlab_widgets` is in the site, so `ipywidgets` work live. Plain imports of common packages install themselves, because Pyodide works from `import` statements in the source; `importlib.import_module` does not trigger that.

`make jupyterlite` builds the pinned 65 MB site and `make jupyterlite-serve` serves it on :4203.
To host it: any static host on its own origin, the app's origin in `JUPYTERLITE_HOST_ORIGINS` at build time, `frame-ancestors` limited to the app, real 404s rather than an `index.html` fallback, and caching that revalidates stable names such as `service-worker.js`.
Python loads from jsDelivr at run time, the kernel's default, rather than self-hosting hundreds of megabytes.

## Alternatives considered

- **CodeMirror 6 instead of Monaco.** Lighter and works on touch. Monaco was chosen for its built-in language services, and phones are view-only anyway.
- **A hosted office engine (OnlyOffice, Collabora Online).** Full fidelity, but AGPL or a server to run and pay for, with every edit passing through our infrastructure.
- **Separate office viewers plus lossy editors.** The first plan paired `docx-preview` with an HTML round trip through `mammoth` and ProseMirror, and rendered slides with text-in-place editing. A spike showed the round trip drops headers, footers, colour, size, alignment and custom styles. That was not the Docs and Sheets experience we wanted, and it meant two rendering paths per format, so the editor became the viewer on Univer and our own slide editor.
- **Rebuilding office files from the model on save.** The first version of the office suite did this, and warned about what it dropped. Real files showed how much that was (masters, animations, styles, theme), so saving moved to patching the original package (section 6).
- **Keeping charts and pivots read-only.** Reasonable for a first version, but they are why many people open a spreadsheet at all. Univer's chart and pivot packages are commercial, so we built them, on the original XML so that nothing we do not model is lost.
- **Interactive slicers on the grid through Univer's float DOM.** It needs a React component or an adapter, so slicers are pictures with their controls in the panel.
- **JupyterLite under `/jupyterlite/`, on the app's origin.** The obvious way to host it, and the first plan, until the shared `sessionStorage` made it a way to steal the session token.
- **A custom JupyterLite contents drive over `postMessage`.** The first plan for Run. Using the default drive with a bridge is much smaller, and it is what lets kernels `open()` the notebook.
- **JupyterLite as the only notebook UI.** It cannot dock in the workspace or render on phones, so it is used for execution only.
- **Executing notebooks on a server.** Real kernels, but it means hosting arbitrary execution, which we ruled out for the in-app notebook.
- **Running script-driven output without asking.** Simpler, but opening a notebook would make outside requests. The click keeps that a choice.
- **`?v=<content_version>` on content URLs.** The first plan for cache busting. Every caller would have needed the version, and revalidating with an ETag fixes all of them.
- **Edits as a new file every time.** Safe, but it is the download-and-re-upload loop in disguise. Overwrite with a version token and an explicit Save As instead.
- **A server push channel for file changes.** The right end state, but there is none in the app, and refreshing on turn completion covers the common case.

## Consequences

- Users can open, read and change every file in a conversation without leaving it, and the agent sees the change on the next turn.
- The initial bundle does not grow. Each viewer is a lazy chunk, and the heaviest (Univer, the TypeScript worker) load only for files that need them.
- We own a lot of OOXML: three package patchers, a chart renderer and a pivot engine. Saved files follow the formats as we understand them, and the risk is a repair prompt in Office rather than lost data.
- `file_attachments` gains `content_version` and `content_sha256` by auto-migration.
- Downloads always make a request, though a current copy costs only a 304.
- There is no history: a bad save is a bad save, and Save As is the escape hatch.

## Known limits

- **Spreadsheets.**
  - Pivots with calculated fields, grouped items, value filters, show-values-as or outside data are kept and refresh in Excel, but cannot be changed here.
  - Editing a pivot uses the compact layout and drops its `formats`, `conditionalFormats` and `chartFormats`, and date items show as serials until Excel refreshes.
  - 3D and combination charts, trendlines and error bars are kept but not editable, and charts draw in a simpler style than Excel's.
  - Timelines are kept but not shown.
  - Comments anchored to drawings are not written, and new comments are authored "WhatIff user", because Univer's current user is not wired to the signed-in user.
  - Renaming a sheet orphans its kept drawing and chart references.
- **Documents.**
  - Table borders cannot be edited here, because Univer has no command for them.
  - Shading and text formatting from table styles are not applied, including banded rows and header-row colours; only their borders are.
  - Tracked changes cannot be accepted or rejected here, new typing is not tracked, and deleted text does not show.
  - Content-control pickers open only in Word, and data-bound controls refresh from the document's data there.
  - Backspace directly after an opaque run or a deletion appears to do nothing; ArrowLeft steps past it.
- **Slides.** Gradient and picture fills, group contents, table geometry, and video and audio are kept but not editable.
- **Notebooks.** Formats before 4 are refused with a reason.
- **Verification.**
  - Saved files have been read by openpyxl, python-docx, python-pptx and LibreOffice, and exercised in Chromium.
  - They have not been opened in Microsoft Office.
  - Slicer XML has been checked only by our own code.
  - Real Bokeh and Altair outputs have not been run in the viewer.
- **Mobile WebView.** Module workers in the Android System WebView are unverified, and Run needs a service worker there, so it is web first.

## Later

- A push channel so the Files tab and an open viewer update while the agent works.
- Version history and undo for saves.
- Letting the agent propose edits to user files for approval, with a diff viewer to show them in.
- Accepting and rejecting tracked changes, clickable slicers on the grid, and shape and media editing in slides.
- An automated licence check in `frontend-pr-validation`.
- A storage quota per account, now that users can create files.

Collaborative cursors or presence in an open file are out of scope.

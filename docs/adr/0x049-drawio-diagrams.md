# ADR 0x049: draw.io diagrams

- **Status:** Accepted
- **Date:** 2026-11-12
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

Mermaid (ADR 0x048) is how models and people write diagrams as text, but it keeps no positions: nothing in it holds a node dragged into place, a sticky note, or a connector routed by hand.
People and agents need a diagram file they can both edit, that covers sequence diagrams, swimlanes, free-form layouts, connectors and notes, that comments can be placed on, and that downloads as the file and as images.

draw.io (diagrams.net) is that file and that editor.
Its format is XML that agents read and write (jgraph publishes a reference written for AI generation), it covers every diagram type above, imports Visio, and exports PNG and SVG that still carry the diagram.
The pinned release's code is under the Apache License 2.0 (some 2024 releases carried an extra clause, so each upgrade re-reads the `LICENSE`), published as built code; its icons and shape libraries have their own terms, and its name is a trademark.

## Decision

### 1. The editor is a pinned draw.io release on a site of its own

- `make diagrams-site` builds the site from one pinned release (`DRAWIO_VERSION`, checked against `DRAWIO_SHA256`): the static web app from the release's `draw.war`, without the Java servlets, source maps and cloud integrations, with its `LICENSE` and a `NOTICE`.
  Upgrading is changing the pair, after reading the new release's `LICENSE`.
- Our settings (`web/diagrams/PreConfig.js`) replace the release's: no export server, no outside services (`stealth`, `offline`), and files saved as plain XML (`compressXml: false`).
- The site is served from an origin other than the app's, as the notebooks site is (ADR 0x043 §11), so draw.io's scripts never run with the app's session: locally `make diagrams-serve` (:4204), deployed `diagrams-dev.whatiff.chat` and `diagrams.whatiff.chat`.
  The app's `diagramsUrl` names it; a URL on the app's own origin, or plain http off localhost, turns diagrams off.
- The embedded editor makes no requests outside its own site.

### 2. `.drawio` files open in the file workspace

- `.drawio` (and `.drawio.xml`, `application/vnd.jgraph.mxfile`) is the `diagram` file kind.
- The editor is embedded in an iframe in embed mode with draw.io's JSON message protocol: once the editor says it is ready it is sent the file, and every change comes back as the whole diagram (`autosave`).
  Messages are taken only from that frame's window and origin.
- One component views and edits (`editInPlace`): viewing shows the diagram with no menus or panels and takes no changes; editing is the full editor, in the app's theme. Saving, Save as, conflicts and closing are the workspace's, as for every editor.
- Files are saved as plain XML, never draw.io's compressed form, so diffs and agents can read them.

### 3. Agents read and change diagrams with their file tools

`.drawio` is among the files agents may create, read and patch (`agent_file_read`, `agent_file_patch`); a user's diagram is changed under the edit grants of ADR 0x046.
It is not indexed for search: the XML is mostly geometry and style.

### 4. Mermaid becomes native shapes

draw.io's `load` takes Mermaid (`descriptor: { format: 'mermaid', data }`) and draws it as native, editable shapes, so a Mermaid diagram can become a `.drawio` file.

### 5. Downloads

Above the diagram, viewed or edited, Download PNG and SVG save the diagram as the editor draws it (its `export`, at twice its size for PNG, on white).
Both images carry the diagram itself, as draw.io's own exports do, so draw.io opens either again as an editable diagram.
The file itself downloads as every file does.

## Consequences

- Users draw and edit diagrams in the workspace, and agents change the same files as text.
- The site is about 118 MB of static files, loaded only when a diagram is opened.
- draw.io's editor is code we host but do not build; anything we add to it (§ Later) must stay small and is checked on every upgrade.

## Later

- **Comments on diagrams and on single elements**: threads beside the file (ADR 0x045 §4) anchored on an element's id, through a small plugin in the editor.
- **A preview image** saved with each diagram, for the gallery and for agents to look at; and PDF, which draw.io makes only with an export server.
- **Open as editable diagram** wherever a Mermaid diagram is drawn (§4).
- **A `diagram` tool** for agents that changes diagrams by element, and the c4a tooling around it.
- **Hosting**: the deployed site's bucket, distribution and `frame-ancestors`, and the deploy workflow's build.

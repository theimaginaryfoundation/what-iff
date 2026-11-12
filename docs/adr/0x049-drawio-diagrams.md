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

### 3. Agents read and change diagrams with their file tools and the `diagram` tool

`.drawio` is among the files agents may create, read and patch (`agent_file_read`, `agent_file_patch`); a user's diagram is changed under the edit grants of ADR 0x046.
It is not indexed for search: the XML is mostly geometry and style.

The `diagram` tool lets agents work by element rather than by patching XML:

- `list` gives each page's elements (id, kind, plain-text label, parent, position and size, connector ends, style) and a text outline; `validate` checks unique ids, the root cells `0` and `1`, that parents and connector ends exist, and that styles are well formed.
  Both work on any of the user's diagrams.
- `create` makes a new diagram of the agent's own from a graph with no positions (nodes, edges, lanes, notes) and lays it out: nodes in ranks along the flow, inside their swimlane, lanes stacked, notes after, left to right or top to bottom.
  The user can tidy it with the editor's own layouts.
- `add_node`, `add_edge`, `add_container` (a swimlane or a group), `add_note`, `set_label`, `set_style`, `move` and `delete` (with what is inside and the connectors left without an end) change one element by id.
- The XML is changed as text where the decoder finds each element, so everything else is kept byte for byte, and a change that would leave the diagram with a new problem is refused.
- It changes files as `agent_file_write` does: the agent's own diagrams, and a user's under an edit grant, saved as a new version.
- Compressed pages are refused, with a hint to save the diagram again from the editor.

### 4. Mermaid becomes native shapes

draw.io's `load` takes Mermaid (`descriptor: { format: 'mermaid', data }`) and draws it as native, editable shapes, so a Mermaid diagram can become a `.drawio` file.

### 5. Downloads

Above the diagram, viewed or edited, Download PNG and SVG save the diagram as the editor draws it (its `export`, at twice its size for PNG, on white).
Both images carry the diagram itself, as draw.io's own exports do, so draw.io opens either again as an editable diagram.
The file itself downloads as every file does.

### 6. Comments on elements

A diagram's comments are kept beside the file, as a text file's are (ADR 0x045 §4), and the same panel shows them.
A thread is on one element, or on the whole diagram:

- The thread names the element by its cell id and its page's id, and quotes its label; it has no range of the text.
  Elements keep their ids when they are moved, restyled or relabelled, so nothing is carried: once the diagram changed, the next read looks each element up again and quotes its label as it is now, and a thread whose element is gone is on the whole diagram with the label it had.
- A small plugin of ours in the editor (`web/diagrams/whatiff-bridge.js`, built into the site's `PostConfig.js`) marks the elements that have threads, the active one more strongly, and reports the page shown, the selection and clicks; right-clicking an element offers New, Reply to and Delete comment, as every other format has (ADR 0x045).
  It uses only draw.io's graph API, and talks to the app in the embed protocol's messages under names of its own.
- Viewing, clicking an element and then Comment starts a thread on it (with none chosen, on the whole diagram), and comments are written as they are made; editing, they are written once a save has stored the diagram, so a thread on a new element finds it there.
- Agents comment with `file_comments`, giving the element's id (`cell`, with `page`); `list` says which element each thread is on.

## Consequences

- Users draw and edit diagrams in the workspace, and agents change the same files as text.
- The site is about 118 MB of static files, loaded only when a diagram is opened.
- draw.io's editor is code we host but do not build; what we add to it (the plugin of §6) stays small and is checked on every upgrade.

## Later

- **A preview image** saved with each diagram, for the gallery and for agents to look at; and PDF, which draw.io makes only with an export server.
- **Open as editable diagram** wherever a Mermaid diagram is drawn (§4).
- **c4a tooling** for diagrams: inspecting, laying out with Graphviz, converting from Mermaid and rendering.
- **Hosting**: the deployed site's bucket, distribution and `frame-ancestors`, and the deploy workflow's build.

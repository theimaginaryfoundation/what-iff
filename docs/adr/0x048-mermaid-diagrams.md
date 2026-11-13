# ADR 0x048: Mermaid diagrams in Markdown

- **Status:** Accepted
- **Date:** 2026-11-12
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

Mermaid is the diagram language models write most reliably, and the one GitHub, Notion and Obsidian draw from a ` ```mermaid ` code fence.
Agents already answer with it, and users keep it in Markdown files, but here it showed as code.

This is the first step towards diagrams people and agents can edit together; an editable diagram file (`.drawio`) is a later decision, and Mermaid is what it can start from.

## Decision

### 1. A Mermaid fence is drawn wherever Markdown is rendered

In chat messages, `.md` files in the viewer and the editor's live preview, and notebook Markdown cells, a fenced code block whose language is `mermaid` is drawn as a diagram, the way LaTeX is drawn (`core/utils/latex-math.helpers.ts`):

- A `marked` renderer (`mermaidExtension`, beside `latexMathExtension`) turns the fence into a `.mermaid-diagram` placeholder holding the escaped source in a `pre`, so Angular's HTML sanitizer stays as strict as it is.
- Once the sanitized HTML is in the page, `renderMermaid(root)` draws each placeholder, where `renderLatexMath(root)` already runs.
- Only a closed fence becomes a placeholder: a reply still streaming in shows the code until the closing fence arrives, instead of a diagram that fails with every chunk.
- Drawings are cached by source and theme, so a message rendered again as it streams does not draw its finished diagrams again.
  Source Mermaid cannot draw is cached as such; a failure to load Mermaid itself (a chunk that did not download) is not, so the next diagram tries again rather than every diagram failing until a reload.
- Diagrams are drawn one at a time; a diagram whose placeholder has gone by its turn (a preview typed into, a reply re-rendered as it streams) is skipped, so drawing never falls behind the text.
- Each drawn copy of a diagram, and its full-screen view, gets ids of its own, so one copy's arrowheads and gradients never point into another copy.

### 2. Drawn safely

- Mermaid (MIT) is loaded on demand with a dynamic `import()`, and loads each diagram type on demand, so none of it is in the initial bundle.
- It runs with `securityLevel: 'strict'` and HTML labels off, so the SVG holds no HTML (`foreignObject`), and with `suppressErrorRendering`, so a failed drawing leaves nothing behind in the page.
- A diagram's own `%%{init}%%` directive cannot change the security level, error rendering, the theme's CSS or variables, fonts or labels.
- The SVG passes through DOMPurify's SVG profile before it is inserted.
  Anything in its CSS that could fetch is removed: `@import`, and `url()`, `image-set()`, `image()` and `src()` to anything but a fragment of the SVG itself (`url(#arrow)`, quoted or not).
  CSS escapes are decoded before that check, and what is inserted is the decoded text, so an escaped `url(` or `@import` cannot get past it.
- Source Mermaid cannot draw stays as code, with "Couldn't draw this diagram."

### 3. It looks like the page

Mermaid's `base` theme is set from the app's theme tokens (`styles/themes.scss`), read as the page resolves them for the light or dark theme, so nodes, lines, notes and series colours match the page.
Node fills and some series colours are tints of the accent that have no token of their own.

### 4. Size, and a toolbar on each diagram

A diagram is drawn at its own width, scaled up on wide screens (1.25× from 1280px, 1.5× from 1800px), never wider than its box nor taller than most of the screen.
Mermaid's `width="100%"` is not used: in a box that shrinks to its content, such as a chat bubble, it falls back to the browser's default 300px.

The toolbar switches to the source and back, opens the diagram full screen, copies the source, and downloads the diagram as SVG or PNG (twice its size, on the page's background).
Copying a selection that holds a diagram copies its source as a ` ```mermaid ` fence (longer than any run of backticks in it) in a `pre`, with its lines kept, as copying a formula copies its TeX.

### 5. Mermaid files

`.mmd` and `.mermaid` files (`text/vnd.mermaid`) are a file kind of their own: the viewer draws the diagram, with the raw text a click away, and the editor is Monaco with a live preview, as for Markdown.
The server accepts them as uploads, indexes them as text, and agents can create them.

### 6. Comments

Comments on text files work on a Mermaid file's source as on any text file (ADR 0x045 §4).
In formatted Markdown, comments are found by their words (ADR 0x045 §4); what is drawn, diagrams and formulas, is left out of those words, so a thread is never placed on a diagram's labels, and a thread on a diagram's source shows in the raw view.

### 7. Resizing a message

On desktop, a chat bubble has a handle on its outer edge (right for the agent's replies, left for the user's messages): dragging it, or the arrow keys on it, sets the bubble's width, and double-click resets it.
The width is kept per message in the browser, so a resized bubble stays resized after a reload, and a resized bubble's diagrams grow with it, up to twice their drawing's width.

## Consequences

- Diagrams agents and users write in Mermaid are drawn in every place Markdown is shown, without anything new for agents to learn.
- The initial bundle is unchanged; the first diagram on a page loads Mermaid.
- Diagram types that need HTML labels lose some layout quality (a mindmap's root label sits off centre), which is the cost of drawing without HTML.
- A drawn diagram keeps the theme it was drawn in until the page renders it again.

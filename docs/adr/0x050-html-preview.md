# ADR 0x050: HTML files and web projects, rendered

- **Status:** Proposed
- **Date:** 2026-11-14
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

Markdown opens rendered in the file workspace (ADR 0x043), with comments placed on the rendering (ADR 0x045).
HTML opens as source only, though a web page is the most common thing an agent is asked to build, and a page is rarely one file: a stylesheet, a script, images, a second page.
People want to see the page, point at a part of it and say what is wrong with it, and have the agent see the same comment at the code that made that part.

Three facts about the app shape the design.
The page runs code the user or an agent wrote, so it must run where the notebooks site and the draw.io editor run: on an origin of its own, never with the app's session (ADR 0x043 §11, 0x049 §1).
The app authenticates with a bearer header, so a page's own requests for its stylesheet or its data cannot reach the API as the user; whatever serves the project's files must already hold them.
And a thread is anchored on a range of the file's text, which the server carries across every change made anywhere (ADR 0x045 §4), so an anchor that is a range of the source keeps working with everything comments already do.

## Decision

### 1. `.html` files open rendered, beside their source

- `.html` and `.htm` (`text/html`, `application/xhtml+xml`) are the `html` file kind: viewed, the page is shown rendered with a toggle to the source, as Markdown is; edited, the source is in the code editor with the rendering beside it, rebuilt once typing pauses (longer than Markdown's pause, as each rebuild runs the page's scripts again).
  Phones view only, as for every editor.
- A page is rendered in an iframe that is never same-origin with the app.
  A file opened on its own, with no project (§2), renders in a frame with `sandbox="allow-scripts"` and nothing else, which gives it an opaque origin, as a notebook's script-driven outputs have: it can reach no cookie, storage or page of the app, open no popup and navigate nothing but itself.
  The files it names beside it in its folder (stylesheets and what they name, scripts, images, media, fonts) are inlined as `data:` URLs, so a stylesheet or a script next to the page still loads.
  Not `blob:` URLs: the browser refuses an opaque-origin frame another origin's blobs.
  This is the whole of what a page gets without a site (§3), and it needs nothing configured: a reference into another folder, a relative import inside a module, or a request the page makes at run time is not resolved, and the page says which names it could not find.
- The rendered page is what the browser makes of the file: no sanitizing, no stripped scripts.
  The frame's isolation is what keeps the app safe, not the page's content.

### 2. A folder opens as a site

- A project is a gallery folder: its files and those of the folders beneath it, with their paths relative to it (`index.html`, `css/app.css`, `img/logo.png`).
  Where the app has a preview site (§3), an `.html` file always opens this way, with its own folder as the project's root, so a page finds its other files as it would on a web server; Open as site, in a folder that has an `index.html`, opens that page.
- Paths in the project are the gallery's: folder segments are lower case (the gallery normalizes them), file names keep their case.
  A reference whose folder case differs from the gallery's still resolves, as folder segments are matched without case, so `Assets/logo.png` finds `assets/logo.png`; a name must match exactly.
- Files are found as the page asks for them, a folder at a time, so a project has no size limit of its own; a folder's listing is reused for a few seconds and each file version is downloaded once.
- **A conversation is a project too.** A page opened in a conversation finds a path among that conversation's files (its context, ADR 0x043 §4, at their workspace paths, §7) before the gallery's: the conversation's project is what its agent's container holds, and a file of the same name elsewhere in the gallery does not stand in for it.
  A path the conversation has no file at is looked for in the gallery folder, as outside a conversation.

### 3. The preview site: an origin of its own, asking the app for each file

- The site is three small files of ours (`web/preview/site`: a host page, its script and a service worker), served as the diagrams site is (ADR 0x049 §1): locally by the app's dev server at `/preview/` on `127.0.0.1`, deployed on a host of its own, built by `make preview-site`.
  The app's `previewUrl` names it; a URL on the app's own origin, or plain http off localhost, leaves projects off and an `.html` file renders on its own (§1).
- The app frames the host page with a random key (`index.html?t=<key>`), one per opening of a page.
  Once the host page's service worker is ready it says so; the app answers with a `MessagePort` and the entry path, and the host page opens the user's page in a frame of its own at `p/<key>/<path>`.
- **Nothing is kept on the site.** Every request under `p/<key>/` reaches the worker, which asks the host page that owns the key, which asks the app over its port; the app finds the file (§2) and answers with its bytes and type, or a 404.
  A browser stops an idle worker and drops what it holds, so a worker given the files would lose them; asking each time keeps it stateless, and a request being answered keeps it alive.
  The host page empties the origin's local and session storage when it starts, as the page it shows may be another project's.
- The page is reloaded when the text being edited changes, and when a file it used changed elsewhere: while it is shown, the app lists the folders it served from every few seconds and compares each served file's content version; a file that was missing and now exists counts too (§6).
  The page's own state (scroll, typed input) is lost, which is accepted.
- The user's page shares the host page's origin (a worker serves only same-origin documents); that origin holds nothing of the app's, and the app's frame has the sandbox the diagrams frame has.
- The host page takes the app's first message only from its parent window, on an origin listed in its `config.js` (`PREVIEW_HOST_ORIGINS` at build time); the app takes messages only from that frame's window on the configured origin.
- The site's response headers: `frame-ancestors` the app only; no caching of the host page or the worker, so a frame never runs an old worker against the app; and a content security policy.
  Scripts may come only from the site itself (inline and `eval` included) and the CDNs of §4, as scripts are where code comes from.
  Styles, images, fonts, media and `fetch` may come from any https host: pages agents build use remote images and public APIs, and the origin holds nothing but the project's own files.
  No plugins, `<base>` only to the site, forms only to the site.
- The host's headers reach the host page only, so the worker reads the site's policy from the host page's headers and sends it on every page it serves, with `'self'` added to `frame-ancestors`, as the host page frames them.
  Where the site sends no policy (local development), neither do its pages.

### 4. Dependencies

- **Bare imports load from esm.sh.** `import { h } from 'preact'` names a package, which a browser cannot resolve; as each module is served (a `.js` or `.mjs` file, or an inline `<script type="module">`), its bare imports are rewritten to `https://esm.sh/<name>@<version><path>`, with the version or range the project's root `package.json` asks for, else the latest.
  Imports are found by `es-module-lexer`, so a string or a comment that looks like one is left alone, and a script that is not a module is served as it is.
  A page that brings its own import map keeps it: the project's modules are then served unchanged.
  Rewriting each module as it is served needs no scan of the project first, which an import map in the page would.
  Nothing is installed; the site's policy allows `esm.sh` and `cdn.jsdelivr.net`.
  This needs the internet, as the notebook kernel's packages do, and is on by default.
- **TypeScript, JSX and SCSS are compiled in the browser.** Where the page asks for a `.ts`, `.mts`, `.tsx` or `.jsx` file, the app compiles it with `esbuild-wasm` before answering; a `.scss` or `.sass` file, with `sass`.
  Each file is compiled on its own, as a dev server does, not bundled: its imports stay imports, which the page asks for in turn.
  JSX uses the automatic runtime of `react`, or of `preact` when the project depends on Preact and not React, and the compiled module's bare imports are rewritten as any module's are.
  Both load the first time a page needs them, on the app's thread (transforming one file is quick, and a worker made from a blob is one the app's policy may refuse), so a project of plain HTML, CSS and JavaScript loads neither.
  Compiled files are kept by file version, a few hundred at most.
  A stylesheet's `@use`, `@forward` and `@import` resolve inside the project as sass does on disk: partials, the extension left out, a folder's `index`.
- **Imports resolve as a bundler's do.** An import without an extension (`./App`, `./components`) is answered with a redirect to the first of `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.mts` and then the folder's `index` of each that the project has, so the file's own imports resolve from where it is.
  A script that imports a stylesheet, JSON or another file (`import './app.scss'`, `import data from './data.json'`) gets a module standing for it: the stylesheet (compiled, if it is SCSS) is added to the page, JSON is its value, anything else is its URL.
  The service worker passes on what the page asked for a file as (`script`, `style`, ...), which is how the app tells a module import from a `<link>`.
- **A compile error shows on the page.** A script that does not compile is served as one that logs the error, with its file, line and column, and throws it; a stylesheet, as one that puts the error at the top of the page.
- **Uploads accept a web project's sources:** `.tsx`, `.mts`, `.jsx`, `.mjs`, `.scss` and `.sass`, as text, searchable like the rest of a project's code.

### 5. Comments on elements

A page's comments are kept with its HTML file as a text file's are (ADR 0x045 §4), in the same panel; a thread on an element shows on the rendered page, and in the source when it has a code anchor.
An element thread has up to two anchors, each best effort, and falls to the whole file when both are lost:

- **The code anchor is a range of the source, as every text thread has.** For an element written in the HTML, it is its opening tag.
  The served page carries, on every element the HTML parser read, the tag's offsets in its file (`data-wi-src="<file>:<start>-<end>"`, in the units comment anchors use), added by the app as the file is sent from a location-aware parse (parse5) and inserted into the text as it is; a pick on the page reads them back, and the thread is made as a text thread on that tag, with the tag as its quote.
  So it shows in the code editor at the tag, the server carries it across edits made anywhere, and `file_comments` reads and writes it with nothing new.
  A thread made on a stretch of the source marks the element whose tag holds its start, or the first tag inside it.
- **The element anchor is a selector for the element on the page** (`element: { page, selector, fingerprint }`, `page` the HTML file's id, beside `anchor` as `cell` is for diagrams), so a thread stays on the element when the code around it is reshaped, and so an element the page made with scripts, which has no tag in the HTML, can carry a thread at all.
  The selector is chosen from the most stable thing the element has, in this order: an `id` that does not look generated; `data-testid`, `data-test`, `data-cy`, `data-id`, `name` or `aria-label`; one or two classes, unique on the page, that are neither utility classes nor hashed; last, a path of positions from the nearest ancestor with a stable id.
  The fingerprint (tag, the start of its text, the attributes the selector used) travels with it: the page shows a thread on the element its selector finds when its tag and text still match, else on the element of that tag whose text is the fingerprint's.
  The server only bounds the anchor; it does not depend on the text, so it is kept as it is across edits.
- **The code behind a scripted element** is recorded, best effort, as the page runs: the bridge script wraps `createElement`, `innerHTML`, `outerHTML` and `insertAdjacentHTML` and keeps the first project file on the stack for each element made.
  That finds hand-written DOM code and template libraries; it does not find the component behind a framework's own DOM building, and a prebuilt bundle gives nothing better than its own call site.
  Frames in the page itself (the bridge, and the page's inline scripts) are skipped, so the first project script on the stack is where the element was made.
- **Picking and marking.** Comment, on the rendered page, enters a picker: the element under the pointer is outlined with its tag and size, as a browser's element picker does, a click picks it, Esc leaves.
  A thread lives with the file its code anchor is in: a pick on an element the page's own file declares starts a thread there, on its tag and on the element; one on an element a project script made starts it in that script, on the line that made it (without its indentation) and on the element; any other pick starts one in the page's file on the element alone, labelled with its tag and the start of its text.
  So the page shows its own file's threads and those of the project's scripts that name it, loaded as it loads them, and the panel shows them together, a script's named after it (`js/main.js · …`); each thread's reply, edit, resolve and delete goes to the file it lives in, and a script's are written at once, as a viewer writes them.
  The bridge script (`page-bridge.ts`, first in every served page's head, in a layer of its own out of the page's styles) draws the marks of open threads on their elements, the active one more strongly, keeps them in place through scrolling, resizing and the page's own changes, and reports picks and clicks on marks to the app; the host page relays both ways.
  A click on a mark chooses its thread, and choosing one in the panel scrolls the page to it; right-clicking a marked element (or its mark) offers Reply and Delete, as every other format has.
- The same works while editing: Pick on page, over the page beside the source, starts a thread from it; the page file's threads are written with the next save, a script's at once.
- Threads on CSS and script files stay where they are: in the code editor.
- Agents comment with `file_comments` as on any text file, quoting the tag or the code; `add` takes `selector` (and `page`, the HTML file, when it is not `file`) for an element, alone or beside a quote, and `list` says which element of which page a thread is on.

### 6. Keeping the page current

- New text in the editor (once typing pauses) is served in place of the stored page, which reloads.
- While a preview is shown, the app lists the folders it served from every few seconds and reloads the page when a file it used has a new `content_version`, or a missing one appears, so an agent's edit shows without a click (§3).
  Gallery lists carry each file's content version for this, as diagram tiles need it for their preview (ADR 0x049 §8).
  A change event from the server replaces the polling when there is one.

### 7. One path, everywhere

A file's path is the same in the agent's file tools, in the Files tab, in a project of §2 and in the container's workspace.

- A conversation's context file's `WorkspacePath` (ADR 0x043 §4) is its folder and name (`notes/journal.md`), no longer its name alone; skill bundles stay under `skills/<slug>/`.
  Newest wins per path as before, so files of one name in two folders no longer shadow each other.
- The file tools say it in one sentence, and nothing else about layout: a file's path is the same everywhere, and `/workspace/<path>` in the container.
- A deployment that runs the workspace in a container must let commands write into folders its uploads created, or an agent cannot add a file to a folder the app put there; that is the deployment's to arrange before paths change.

### 8. A picture of each page

A page has a picture, as a diagram has (ADR 0x049 §8), so an agent can see what it built and the gallery can show it.

- The page draws it: when a page is shown and has settled, the app asks the bridge script (§5) for a picture, and the bridge draws its document with `modern-screenshot` (MIT, loaded from jsDelivr when first asked) at the frame's width and up to 3000 pixels of its height.
  Nothing outside the page sees into it, which keeps the preview site's origin of its own (§3) intact.
- The app stores it as the file's preview, as for a diagram, when the page's file has a new version; a page that does not answer in 20 seconds, or cannot draw itself, has none.
- `agent_file_read` shows an HTML file's picture with its text, from the stored preview; the gallery's tile shows it too.
- It is the page as the user's browser drew it, last time someone looked: an agent that changed a page sees the new picture only once the page is shown again.

## Alternatives considered

- **Rendering in a `srcdoc` frame only**, with every reference rewritten to a `blob:` URL.
  It is what §1 does for a lone file, and it stops where a page asks for a file by a string: `fetch('./data.json')`, a dynamic import, a worker, a second page.
  No service worker can serve such a frame, as its origin is opaque.
- **The API serving the project** on a host of its own, with a short-lived cookie or a capability in the path.
  It would give previews a URL of their own, which nothing needs yet, at the price of a second way to be the user, a round trip per file on every change, and a server that assembles trees.
- **A hostname per project**, as hosted sandboxes do.
  Stronger isolation between projects than one site with a token per frame, for a wildcard certificate and a DNS entry per host; the per-frame token keeps projects apart in the one tab where they could meet, which is as far as the risk reaches.
- **Node in the browser** (WebContainers, Nodebox).
  Both need a licence for production use in a product; neither is needed once a toolchain can run in the conversation's container.
  Sandpack's bundler is Apache-2.0 and self-hostable, and would shape the preview around a bundler for every project; its static server is the service-worker pattern of §3, read for its design.
- **One anchor** (a selector only, or a source range only).
  A selector alone says nothing in the code editor and cannot be carried by the server across edits; a source range alone cannot hold a thread on an element a script made.
  Two anchors, each best effort, cover both, and lose gracefully to the whole file.

## Consequences

- Users see the pages agents build, as they are, and comment on the parts; agents see the same comments at the code.
- The page runs on the preview site's origin with no credentials of the user's: a page that calls the API gets a 401, which is the point.
- The app gains a small static site to build and host, as it has two already, and a service worker of its own, which has lifecycle to test: the first load before the worker controls the page, a frame closed with its tree, an update of the worker itself.
- Projects that import from a CDN need the internet, as does a page's picture (§8); the rest, compiling included, works without it.
- Paths with folders in the container change what agents see at `/workspace`; existing containers take the new layout at their next hydration.

## Later

- **Builds in the container**: a project with a `vite.config.*` or a build script built where agents run commands, and its output folder served as the site.
  The container has no network and no Node today, so this waits on both; nothing of Node runs in the browser either way.
- **Comments on elements a compiled script made** (§5): such a thread keeps its element anchor but not a code anchor, as the call site the bridge records is in the compiled module; mapping it through the module's source map would place it on the `.tsx` line.
- **A picture drawn without a viewer**, so an agent sees a page it changed before anyone opens it.
- **Hosting**: the deployed site's bucket, distribution, response headers and the deploy workflow's build.

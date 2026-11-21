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
  Open as site on a folder, or on an `.html` file in one, opens the project with that file as its entry (`index.html` when a folder was opened).
- Paths in the project are the gallery's: folder segments are lower case (the gallery normalizes them), file names keep their case.
  A reference whose folder case differs from the gallery's still resolves, matched case-insensitively on folder segments, so `Assets/logo.png` finds `assets/logo.png`; a name must match exactly.
- A conversation's files are a project too: the files in its context (ADR 0x043 §4), at their workspace paths (§7).
  A page open from a conversation finds its stylesheet and its script there the same way.
- The project's files are the text and image files the workspace already serves; anything else (a notebook, a spreadsheet) is served as bytes with its type and no more.
  A project is at most a few hundred files and a few tens of MB; past that, Open as site says so.

### 3. The preview site: an origin of its own, serving what the app sends it

- `make preview-site` builds a small static site of ours, served as the diagrams site is (ADR 0x049 §1): locally by the app's dev server at `/preview/` on `127.0.0.1`, deployed on a host of its own.
  The app's `previewUrl` names it; a URL on the app's own origin, or plain http off localhost, leaves projects off (a lone `.html` still renders as §1).
- The site's page registers a service worker and waits for the app.
  The app opens it in an iframe, hands it a `MessagePort`, and sends the project's files; the worker answers every request under `/p/<token>/<path>` from that tree, with the file's type, and a 404 for what is not there.
  The token is random per frame, so two previews open in one tab never see each other's files, and a frame's tree is dropped when its frame closes.
- The app pushes a file again when it is saved and tells the frame to reload; the page's own state (scroll, typed input) is lost, which is accepted.
- The site's response headers: `frame-ancestors` the app only, a content security policy that allows the page's own files, inline scripts and styles (a project's pages have them), `data:` and `blob:`, and the CDNs of §4; nothing else.
  The service worker and the site's page are the only code of ours there; the page it serves is the user's.
- Messages between the app and the frame are taken only from the configured origin and that frame's window, as for the notebooks and diagrams sites.

### 4. Dependencies

- **Bare imports resolve through an import map.** `import { h } from 'preact'` in the project's modules is resolved by a map the app injects into the served page: each bare specifier to `https://esm.sh/<name>@<version>`, the version from the project's `package.json` when it has one, else the latest.
  Nothing is installed; the site's policy allows `esm.sh` and `cdn.jsdelivr.net`, the two hosts people's pages already use.
  This needs the internet, as the notebook kernel's packages do; the app is on by default and says so where the import map is explained.
- **TypeScript, JSX and SCSS are compiled in the browser.** Where the served page asks for `app.tsx` or `styles.scss`, the worker asks the app, which compiles it with `esbuild-wasm` or `sass` in a web worker and answers; compiled files are cached by content.
  This is a later step (see Later) and adds nothing to a project of plain HTML, CSS and JavaScript.
- **Real toolchains run in the conversation's container.** A project with a `vite.config.*` or a build script is built where agents run commands, and its output folder is what the site serves; nothing of Node runs in the browser.
  This is for deployments that run containers, as the diagram workshop is (ADR 0x049 §3), and a later step.

### 5. Comments on elements

A page's comments are kept beside its files as a text file's are (ADR 0x045 §4), in the same panel; a thread on an element shows on the rendered page and in the source.
An element thread has up to two anchors, each best effort, and falls to the whole file when both are lost:

- **The code anchor is a range of the source, as every text thread has.** For an element written in the HTML, it is its opening tag.
  The served page carries, on every element the HTML parser read, the tag's offsets in its file (`data-wi-src="<file>:<start>-<end>"`, added by the app as the file is sent, from a location-aware parse); a pick on the page reads them back, and the thread is made as a text thread on that tag, with the tag as its quote.
  So it shows in the code editor at the tag, the server carries it across edits made anywhere, and `file_comments` reads and writes it with nothing new.
- **The element anchor is a selector for the element on the page** (`element: { page, selector, fingerprint }`, beside `anchor` as `cell` is for diagrams), so a thread stays on the element when the code around it is reshaped, and so an element the page made with scripts, which has no tag in the HTML, can carry a thread at all.
  The selector is chosen from the most stable thing the element has, in this order: an `id` that does not look generated; `data-testid`, `data-id`, `name` or `aria-label`; a class combination unique on the page that is not utility classes; last, a structural path with positions.
  The fingerprint (tag, trimmed text, the attributes the selector used) travels with it: when the page is shown, the selector is checked against the fingerprint, and a selector that no longer points at the same thing is remade from the next strategy before the thread is given up.
- **Finding the code that made a scripted element** is layered by what the app can know.
  An element from the HTML has its tag.
  A JSX, TSX or Vue component the app compiled (§4) has the element's source position added by that compile, the same attribute.
  A scripted element the app did not compile gets, best effort, the first project file on the stack when it was created: the served page wraps `createElement`, `innerHTML` and `insertAdjacentHTML` to record it.
  That finds hand-written DOM code and template libraries; it does not find the component behind a framework's own DOM building, which leaves such an element with only its element anchor.
  A prebuilt bundle gives nothing better than its own call site.
- A thread lives with the file its code anchor is in (the HTML, or the script that made the element), and names the page it shows on.
  The rendered page shows the threads of every file in the project that name it; the panel shows the entry file's threads and switches to another file's when a pick lands there.
- **Picking and marking.** Add comment on the rendered page enters a select mode: the element under the pointer is outlined with its tag and size, as a browser's element picker does, a click picks it, Esc leaves.
  A small script of ours in the served page draws the markers of open threads on their elements (the active one more strongly), keeps them in place through scrolling, resizing and the page's own changes, and reports picks and clicks on markers to the app in messages under names of ours, as the draw.io plugin does (ADR 0x049 §6).
  Right-clicking a marked element offers Reply and Delete, as every other format has.
- Threads on CSS and script files that have no element stay where they are: in the code editor.
- Agents comment with `file_comments` as on any text file, quoting the tag or the code; `list` says which page and element a thread is on when it has one.
  An agent writing a thread on an element it cannot quote (one a framework made) gives its selector.

### 6. Keeping the page current

- A save in the editor pushes the file and reloads the frame.
- While a preview is open, the app asks for the project's files every few seconds and pushes any whose `content_version` moved, so an agent's edit in the conversation shows without a click; the list is one request, which the Files tab already makes.
  A change event from the server replaces the polling when there is one.

### 7. One path, everywhere

A file's path is the same in the agent's file tools, in the Files tab, in a project of §2 and in the container's workspace.

- A conversation's context file's `WorkspacePath` (ADR 0x043 §4) is its folder and name (`notes/journal.md`), no longer its name alone; skill bundles stay under `skills/<slug>/`.
  Newest wins per path as before, so files of one name in two folders no longer shadow each other.
- The file tools say it in one sentence, and nothing else about layout: a file's path is the same everywhere, and `/workspace/<path>` in the container.
- A deployment that runs the workspace in a container must let commands write into folders its uploads created, or an agent cannot add a file to a folder the app put there; that is the deployment's to arrange before paths change.

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
- Projects that import from a CDN need the internet; the rest works without it.
- Paths with folders in the container change what agents see at `/workspace`; existing containers take the new layout at their next hydration.

## Later

- **Compiling in the browser** (§4): `esbuild-wasm` for TypeScript and JSX, `sass` for SCSS, on demand, in a worker; source positions added to compiled components for §5.
- **Builds in the container** (§4), and a built project's output served as the site.
- **A picture of the page for agents**, drawn where a browser runs beside the conversation, as diagrams have a preview (ADR 0x049 §8): the `diagram` tool's shape, for pages.
- **Hosting**: the deployed site's bucket, distribution, response headers and the deploy workflow's build.

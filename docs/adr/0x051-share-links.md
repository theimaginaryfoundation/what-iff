# ADR 0x051: Share links

- **Status:** Accepted
- **Date:** 2026-11-14
- **Deciders:** What Iff maintainers
- **Issue:** none (raised in conversation)
- **PR:** see the pull request that adds this file

## Context

What people make in the app (an image, a document, a notebook, a web app an agent built) can be seen only by them, signed in.
They want to show it to someone without an account: send a link, and the other person sees it.

Four facts shape the design.
A shared item is the user's own content, often written by an agent from untrusted input, so whatever shows it must run where user content runs: on a domain that holds nothing of the app's (ADR 0x050 §3), never on the app's site.
A web app runs code, which must not reach anything a visitor holds for another share, so it runs on the web apps site (ADR 0x050 §3), framed, not on the site that shows the share.
A link is the only thing standing between the content and the internet, so it must be unguessable, carry nothing that names the user or the file, and be revocable.
And a passcode can be guessed, so guessing has to be slow and costly.

## Decision

### 1. What can be shared, and what a link names

- **Every file the file workspace shows can be shared, one link per file, off by default**: text and code, Markdown and Mermaid, HTML, CSV and spreadsheets, documents, slides, notebooks, images and PDFs.
  Diagrams are not shared yet (their viewer is the editor's site, which only the app may frame); nor are a skill's files, which belong to the skill.
  A chat's copy of a file shares the original, as comments and edit grants do (ADR 0x045, 0x046).
- **An HTML file in a gallery folder is shared as a web app**: the page and every file of its project, the page's folder and the folders inside it (ADR 0x050 §2).
  A page at the gallery's top level is shared as a single file: the top level is the user's whole gallery, never a project.
- **A link is live**: it shows the file, or the project's files, as they are when it is opened, so an edit reaches it and a file added to a shared web app's folder is shared with it; the Share dialog says so.
  The project follows the page: moving its folder moves the share, moving the page to the top level makes it a single-file share, deleting the page deletes the share.
- **The link is random**: `https://<share site>/s/<key>`, the key 128 random bits in base64url (22 characters), made when the item is shared and unique.
  It names neither the user nor the file; the server finds the share by its key.
  Stopping sharing deletes the share, and sharing again makes a new key, so an old link never comes back.

### 2. The owner's side

- **Share**, in the file workspace's header, opens a dialog: share the file (with a passcode, the default), then copy the link and the passcode, require a passcode or not, make a new passcode, or stop sharing.
  A web app's dialog names the folder it shares and says that files added to it later are shared too.
- **Shared links**, in Config, lists every share of the user's, with the item, its folder, whether it needs a passcode and when it was made, to copy a link or stop sharing.
- The API (signed in): `GET`/`PUT`/`PATCH`/`DELETE /share-links/files/{fileId}` reads, makes (idempotently), changes and deletes a file's share; `GET /share-links` lists them.
  The app's `shareUrl` (a build setting, as `previewUrl` is) is the share site's address; without it, Share is not offered.

### 3. Passcodes, and what a visitor holds

- **A passcode is 12 characters of `A-Z`, `a-z` and `0-9`, case-sensitive, made by the server** from a cryptographic source without bias (71 bits).
  Users choose whether a share needs one and can have a new one made; they cannot set one themselves, so every passcode is as strong as the next.
- It is stored encrypted, with the key the server keeps tokens with (`TOKEN_ENCRYPTION_SECRET`), so the owner can see and copy it at any time; a server without that key makes only shares without passcodes, and says so.
- **Unlocking** (`POST /shared/{key}/unlock`) compares in constant time and answers with a cookie: HttpOnly, `Secure`, `SameSite=Strict`, scoped to that share's API path, and good for 12 hours.
  It is an HMAC, under a key derived from the server's signing secret, of the share's key, its passcode generation and the expiry; making a new passcode, or turning the passcode on, starts a new generation, so everyone who unlocked before is out.
  Nothing a visitor holds can be read by a script, so a page that runs code (a web app's, §6) cannot take another share's access.
- Until a share is unlocked, the server says only that it needs a passcode: not its name, its kind or its size.

### 4. Guessing is slow, and banned

- **Unlock attempts are limited by address**: an address (an IPv4 address, or an IPv6 /64, as one machine has a whole /64) may try 10 times a minute across all shares.
- **Failures ban the address**: 5 wrong passcodes within 15 minutes, on any shares, ban it for an hour; another 5 within a day of that ban, for a day.
  A banned address is answered `429` with `Retry-After` for every unlock, right passcode or not, so a ban cannot be probed.
- Attempts and bans are kept in the database, not in a server's memory, so every server of a deployment enforces the same ones and a restart does not forgive.
  Addresses are stored as keyed hashes, not as they are, and old attempts are deleted as they age out.
- **The visitor's address is the connection's**, unless the deployment names a header its proxy sets and a visitor cannot (`CLIENT_IP_HEADER`, such as CloudFront's `CloudFront-Viewer-Address`); a header anyone can send (`X-Forwarded-For` as a client wrote it) is never trusted.
- 71 bits cannot be guessed at 5 tries an hour per address; the limits are there so that guessing is pointless, not merely slow.

### 5. The share site

- **A site of its own on the user-content domain** (as `webapps` is, ADR 0x050 §3): a small read-only app, built from the app's code as a second Angular application (`share`) with the app's viewers, and nothing of its account, chats or agents.
  `/s/<key>` asks the API for the share, asks for the passcode when it needs one, and shows the item; a wrong passcode says so, and a ban says how long it lasts.
- **The API is reached on the share site's own origin** (`/api/shared/...`, which the deployment routes to the API), so the unlock cookie is first-party and the site needs no CORS.
  The public routes (no sign-in): `GET /shared/{key}` (the share, or that it is locked), `POST /shared/{key}/unlock`, `GET /shared/{key}/content` (a single file's bytes), `GET /shared/{key}/files` (a web app's files) and `GET /shared/{key}/files/{path}` (one of them).
  A share that does not exist, and one whose file the owner has lost access to, are a `404`; a locked one is a `401`.
- **Content is served so that it cannot run on the share site**: `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff` and a policy of `sandbox; default-src 'none'`, so opening a content URL directly downloads it, and an HTML file never renders as the share site's own page.
  The site fetches the bytes and hands them to a viewer, as the app does.
- **Every public answer is for the share site alone**: `Cache-Control: private, no-store` (a link that stops being shared stops working at once) and `Cross-Origin-Resource-Policy: same-origin`.
  The web apps site and the share site are on one registrable domain, so they are the same site to a browser and a shared web app's requests to the share site carry the visitor's cookies; the policy keeps such a page from loading another share the visitor unlocked as a script or an image, and a response of another origin is opaque to it.
  It can set a cookie of the same name on the parent domain, which is not an unlock (its HMAC does not hold), so the server tries every cookie of that name.
- The site's response headers: no framing by anyone (`frame-ancestors 'none'`), scripts from itself only, frames only of the web apps site, no referrer, and no indexing (also in the page's own `<meta>`), as a link is a secret.
- Unlike the app (ADR 0x050 §3), the share site takes a web apps site on its own registrable domain: it only needs another origin, as the site holds nothing a page could take.
- Locally, `npm run start:share` serves it on `localhost:4204`, with `/api/shared` passed to the API; the web apps site of local development (`127.0.0.1:4200/preview/`) accepts it as a parent.

### 6. Viewers, read-only

- The share site shows an item with the app's viewer for its kind, read-only: no editing, no comments, no agent, no markup.
  The viewers reach for the signed-in user's services; on the share site those are stand-ins that read and send nothing (no comments, no page pictures), and the share's files stand in for the gallery's.
  An HTML file shared on its own (§1) shows rendered, with its code a click away, as in the app; a notebook shows its cells and their saved outputs, without running them; office files show in the office viewers, read-only; an image and a PDF show as they do in the app.
  A download button gives the file as it is.

### 7. A shared web app

- **The project is the page's folder and the folders inside it, and nothing else, enforced by the server**: a path is resolved only among the owner's gallery files whose folder is the share's folder or one inside it.
  A path is relative, in `/`-separated segments; an empty segment, `.`, `..`, a backslash, a control character or a leading `/` is refused, never resolved, and folder segments are matched as the gallery keeps them (lower case, ADR 0x050 §2).
  A skill's files and chats' copies are never in it; the listing is the same set, so nothing a visitor sees or asks for comes from outside the folder.
  The share site's own paths are as the server answers them; it cannot widen what the server will serve.
- **Preview and Code**: the share opens on Preview, the page running as it does in the app, on the web apps site (ADR 0x050 §3), whose host page now also accepts the share site as its parent; the share site answers its worker's requests from the share's files, so the page's own requests are confined to the project the same way.
  Code shows a file tree of the project (folders to open and close) and the chosen file: text and code in the code editor, read-only and alone (no preview beside it, no comments), an image or a document in its viewer; choosing an HTML file there and switching to Preview shows that page.
  The listing is read once and reused for 30 seconds, so a preview checking for changes does not ask for it every few seconds.
- TypeScript, JSX and SCSS compile in the visitor's browser (ADR 0x050 §4), with the compiler served by the share site.

## Alternatives considered

- **Links with the file's id and a signature**: they name the file, cannot be changed without changing the file, and cannot be revoked one by one. Random keys in the database can.
- **Snapshots**: sharing a frozen copy is safer for folders, but asks the owner to publish every change and keeps copies of files; a live link, with the dialog saying what it shares, was chosen.
- **Passcodes stored as hashes, shown once**: stronger at rest, but the owner who forgets it has to make a new one and sign everyone out. Encrypted with the key that already protects tokens, it stays at the owner's hand.
- **User-chosen passcodes**: weak ones would be the norm; generated ones are all 71 bits.
- **A bearer token in the share site's storage instead of a cookie**: a script on that origin could read it; an HttpOnly cookie, scoped to one share, it cannot.
- **Rate limits in memory**: they would not hold across servers or restarts.
- **Shares on the app's own domain**: a shared page would run beside the app's session, as ADR 0x050 §3 explains.

## Consequences

- People without accounts can see what users make; what they see is current, so an owner shares a folder's future files too.
- Deployments gain one more static site, a route from it to the API, and a header to name the visitor's address; the web apps site accepts the share site as a parent.
- The database gains shares, unlock attempts and bans; attempts are pruned as they age.
- The share site reuses the app's viewers, so a viewer that reaches for the user's session (comments, the agent) must work without it.

## Later

- Diagrams, through their stored picture or a viewer of their own.
- Expiry dates, view counts, and an owner's notice of failed attempts on a share.
- Sharing a folder that is not a web app (a gallery of images).

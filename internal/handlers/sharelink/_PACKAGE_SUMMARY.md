# Package: `internal/handlers/sharelink`

## Role

HTTP API for **share links** (ADR 0x051): the owner's routes to share a file and manage its link, and the public routes the share site reads a shared file or web app through, without signing in.

## Responsibilities

- **Owner routes (signed in, `owner.go`):** `GET /share-links` lists the user's links; `GET`/`PUT`/`PATCH`/`DELETE /share-links/files/{fileId}` reads, makes (idempotently; 201 when made), changes and deletes a file's link.
  `PUT` takes `passcode_required` (default true); `PATCH` takes `passcode_required` and `new_passcode`.
  A skill's file is 400; a passcode on a server without `TOKEN_ENCRYPTION_SECRET` is 409.
- **Public routes (`public.go`):** `GET /shared/{key}`, `POST /shared/{key}/unlock`, `GET /shared/{key}/content`, `GET /shared/{key}/files` and `GET /shared/{key}/files/{path}`.
  A link that needs its passcode is 401 (`{"locked":true}`, nothing about the item) until the request carries a live unlock cookie.
  A web app's routes are 404 for a single-file link; a single file is `/content`.
- **Unlocking (`session.go`, `gate.go`, `address.go`):** a passcode is compared in constant time; the right one sets `wi_share`, an HMAC of the key, the passcode generation and the expiry (12 hours), HttpOnly, Secure, `SameSite=Strict`, with `Path=/api/shared/{key}`.
  Tries are counted per address (an IPv4 address or an IPv6 /64, stored as a keyed hash): 10 a minute; 5 wrong within 15 minutes ban it for an hour, the next ban within a day for a day; a banned address is 429 with `Retry-After`, right passcode or not.
  A key that names no link counts as a wrong guess.
- **The project lock (`paths.go`):** a web app's path is resolved only inside the page's folder: segments split on `/`, and an empty one, `.`, `..`, a backslash, a control character or a leading `/` is refused (400); folders are lower-cased as the gallery keeps them; the datastore query is bounded to the folder too.

## Dependencies

- **Inbound:** `internal/server` (`RegisterRoutes` on the authenticated router, `RegisterPublicRoutes` on the API router).
- **Outbound:** `internal/datastore` (through `Store`), `internal/storage` (`ReadAttachment`), `internal/models`, `mux`.

## Non-obvious decisions

- **Content cannot run on the share site.**
  Bytes are served with `Content-Disposition: attachment`, `nosniff` and `Content-Security-Policy: sandbox; default-src 'none'`; every public answer is `private, no-store` and `Cross-Origin-Resource-Policy: same-origin`, so a shared web app on the web apps site (the same site) cannot include another share the visitor unlocked as a script or image.
- **The visitor's address** is the connection's unless `Config.ClientIPHeader` (`CLIENT_IP_HEADER`) names a header the deployment's proxy sets and a visitor cannot (CloudFront-Viewer-Address, `ip:port`, IPv6 unbracketed); a header named but missing falls back to the connection's.
- **Keys are derived, not reused**: the cookie key and the address-hash key are HMACs of `JWT_SECRET` with their own labels.
- **A web app follows its page**: the project root is the page's folder at each request, so moving the folder moves the share and moving the page to the top level makes it a single file.

## Testing

- `helpers_test.go`: path resolution (traversal refused), address parsing (CloudFront's IPv6 form, /64), cookies (generation, key, expiry, forgery), and the ban and rate policy against an in-memory gate.
- `handler_test.go`: the public flow through the router, locked to unlocked, the folder lock with encoded traversal, the response headers, a ban that refuses even the right passcode, and a new passcode signing visitors out.

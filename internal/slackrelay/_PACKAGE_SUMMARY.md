# Package: `internal/slackrelay`

## Role

The Slack relay's logic and wire client (ADR 0x032): a persona is a member of a Slack workspace as its own Slack app, mentions in bound channels become turns in their relay threads, and replies are posted back by the persona's bot.
It talks to Slack through the `Slack` interface (the Web API client `API`) and the Socket Mode `Socket`, and to storage through `Store`, so the service is tested without either.

## Responsibilities

- **Access (`access.go`):** `Enabled` is the operator switch (`SLACK_RELAY_ENABLED=true`, default off), on for every user when set.
  The routes, the gateway and `post_to_slack` all ask it.
- **Web API (`api.go`):** form POSTs with the token in the header: `auth.test`, `bots.info`, `apps.connections.open`, `chat.postMessage` (with `link_names` off), reactions, `users.info`, `conversations.list`/`info`/`join`/`replies`, and files (`Download` from Slack's file host only; `UploadFile` in Slack's three steps).
  `Error` carries Slack's error code (and `Retry-After` on a 429); `IsAuthError`, `IsChannelError` and `IsTransient` classify it.
  Every connection goes through the public-address dial guard, and the bot token is sent to `files.slack.com` and nowhere else.
- **Socket Mode (`socket.go`):** one WebSocket per app: open a URL, wait for `hello`, acknowledge every envelope before handling it, ping, and reconnect on `disconnect` (Slack's periodic refresh) or a lost connection, with backoff.
  A refused app-level token stops the session (`SocketInvalidToken`).
  `SocketAppID` reads the app id from a `hello`, which is how the routes check two pasted tokens are one app's.
- **Events (`events.go`):** `EventCallback`, `Message` (thread root, whether it is in a thread, its time from `ts`, mentions, plain subtypes, from a bot), `File`, `ChannelRef`.
- **Text (`text.go`):** `PlainText` turns Slack markup into plain text for the model (the persona's mention removed, other mentions by name, channels, links, entities); `PromptText` is the relayed line (`alice (Slack, #general): text`, with a thread's first message quoted); `ToMrkdwn` converts a reply's Markdown into escaped Slack mrkdwn.
- **Manifest (`manifest.go`):** `Manifest` (the persona's app: its name, Socket Mode on, token rotation off, `BotScopes`, `BotEvents`), `CreateAppURL`, `AppSettingsURL`.
- **Service (`service.go`; the batch path in `inbound.go`, files in `files_in.go`, the name cache in `names.go`):** `HandleEvent` takes an app's events.
  Messages pass `admits` (something someone wrote in a bound channel, newer than the binding, not the persona's own nor an integration's, allowed by the lists, a mention or a thread continuation, bots only with `answer_agents`) and the fail-closed thread gate, then the turn queue (`relaykit.TurnQueue`: one batch per Slack thread, the channel's top-level messages sharing one).
  `channel_rename`/`group_rename` rename the bindings, `channel_left`/`group_left` break them, and `app_uninstalled`/`tokens_revoked` (for the bot) mark the app `invalid_token`.
  `runInbound` re-checks the binding, applies the loop guard (`guardBatch`), records each mention once (the dedupe key), builds the turn (`buildTurn`), starts it metered as `slack`, puts the `:eyes:` reaction on the newest mention while it runs, and posts a neutral notice when it fails (re-reading the binding first, so nothing is posted once it is paused).
  An agent's mention past the loop guard's cap is dropped before it is queued, a failed read of the previous link counts as the chain at its cap, and a batch's links are attached newest first.
  `OnReply` (the reply hook) posts into the newest asker's thread, and to bindings the composer toggle or `post_to_slack` armed (a new message, with further parts and files in its thread).

## Key types and entry points

- `Enabled`.
- `API`, `NewAPI`, `Error`, `IsAuthError`, `IsChannelError`, `IsTransient`, `Identity`, `BotInfo`, `User`, `Channel`.
- `Socket`, `OpenSocket`, `SocketConfig`, `SocketState`, `Opener`, `NewDialer`, `SocketAppID`.
- `EventCallback`, `Message`, `File`, `PlainText`, `PromptText`, `ToMrkdwn`, `MaxMessageRunes`.
- `Manifest`, `CreateAppURL`, `AppSettingsURL`, `BotScopes`, `BotEvents`.
- `Service`, `Store`, `Slack`, `TurnSource`, `NoticeFailed`, `NoticeBusy`, `WorkingReaction`, `UnrestrictedWarning`, `ThreadDeletedError`, `RemovedFromChannel`, `AppUninstalledError`.

## Dependencies

- **Inbound:** `internal/slackplugin` (routes, gateway, reply hook), `internal/agent` (`Enabled` for `post_to_slack`).
- **Outbound:** `internal/relaykit` (turn queue, loop guard, reply bookkeeping, thread gate, reply files, splitter, dial guard), `internal/plugins`, `internal/replyhook`, `internal/featuregate`, `internal/models`, `github.com/gorilla/websocket`, `zap`.

## Non-obvious decisions

- **Model output never notifies anyone.**
  `ToMrkdwn` escapes `&`, `<` and `>` everywhere and builds only http(s) and mailto links, so a reply cannot form `<!channel>`, `<!here>` or `<@U…>`; `link_names` stays off.
  Slack notifies the people following the thread, which includes whoever asked, so the relay adds no mention of its own.
- **Acknowledge first.**
  Slack redelivers an envelope that is not acknowledged within a few seconds, so the socket acknowledges before handing the event on; redeliveries are deduplicated on the message `ts`.
- **No subscription per channel.**
  Slack sends an app every message of every channel it is in, so the gateway's fingerprint is the tokens alone and the binding is looked up per event: binding a channel needs no reconnect.
- **Thread continuations** need no mention when the persona posted in that thread and answered the same author there (`Store.SlackThreadContinues`); Slack has no reply to a single message inside a thread, and a batched turn replies in one thread only.
- **`app_mention` is not subscribed**: a mention also arrives as a message event, and both would answer it twice.
- **Integrations are never answered**: a message with no user behind it (an incoming webhook's `bot_message`) has nobody to answer, and no notice is ever posted to a bot.
- **Messages older than their binding are never answered.**

## Testing

- `service_test.go` drives the whole flow against fake `Store`, `Slack` and turn starter (trigger rules, batching, the loop guard, threads and quotes, the thread gate, post failures and retries, composer posts with long replies and files, channel and app events, inbound files).
- `api_test.go` runs the client against an httptest Web API (auth, errors and rate limits, form fields, channel pages, the file-host guard, three-step uploads); `socket_test.go` against an httptest Socket Mode server (acknowledgements, refresh, a refused token, the hello's app id).
- `text_test.go` covers both conversions, including that no reply can ping anyone; `manifest_test.go` the manifest and its link.

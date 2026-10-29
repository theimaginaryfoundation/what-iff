# Mullet mode

**Business up front, party in the back.**
Talk to your personas on [whatiff.chat](https://whatiff.chat) when you are out, on your phone, wherever.
Your own What Iff server at home keeps a copy of every one of those conversations, so when you sit down at home they are already there, ready to continue on your own server, with your own models.

> **Beta.** Mullet mode is new. It works, it is careful with your data, and it will still have rough edges. Tell us what feels weird: [open an issue](https://github.com/theimaginaryfoundation/what-iff/issues/new/choose).

In the app, the code and the settings it is called **Remote sync**.
The design and the reasoning behind it are in [ADR 0x033](../adr/0x033-mullet-mode-remote-sync.md).

## Why you would want it

- **Your own models.** Continue a conversation on a model you run yourself (Ollama, LM Studio, anything OpenAI-compatible), or with your own provider API keys.
  The hosted app is convenient on the go; at home you are not limited to its model list or its pricing.
- **A privacy boundary you choose.** Everyday chat can live on the hosted app.
  The conversations you would rather keep to yourself can carry on at home, where everything you add stays on your server.
  Mullet mode never sends anything back: what you write at home is never uploaded to whatiff.chat.
- **Saving money.** Use your hosted allowance where it is convenient, and run long or heavy conversations at home on hardware or keys you already pay for.
- **A copy you own.** Every thread you have on whatiff.chat, kept on a machine you control, automatically, not just when you remember to export.

## How it works, in one minute

- Your server pulls your threads from whatiff.chat using a **read-only token** you create there.
  Copies show up as ordinary threads in your thread list.
- It syncs **when you use it**: opening the app at home, or coming back to it after a while, fetches whatever is new.
  It also syncs quietly about once an hour in the background.
  There is a **Sync now** button for the impatient.
- **Continue anywhere.** Reply to a copied thread at home and it just works; the first reply catches the persona up on the earlier conversation.
- **Both sides is fine.** If you continue the same thread at home *and* on whatiff.chat, nothing is merged or lost: your home thread stays exactly as you left it, and the whatiff.chat continuation arrives as a second thread named like `Trip planning (from api.whatiff.chat)`.
  From then on, that second thread is the one that keeps up with whatiff.chat.
- **Nothing is ever deleted or changed by sync.** Delete a copy at home and it stays deleted (sync will not bring it back).
  Deleting or editing something on whatiff.chat does not touch your copy.

## What you need

- A self-hosted What Iff server (see the [quick start](../../README.md#-quick-start-docker)).
- A whatiff.chat account.

## Set it up

### 1. Turn it on for your server

With Docker Compose, mullet mode is **on by default**; there is nothing to do.

Anywhere else, set this in your server's environment (or `.env`) and restart:

```bash
REMOTE_SYNC_ENABLED=true
```

Your server also needs `TOKEN_ENCRYPTION_SECRET` (the compose stack generates one), because the token you paste is stored encrypted.

### 2. Create a token on whatiff.chat

1. Sign in at [app.whatiff.chat](https://app.whatiff.chat) and open **Tools → Webhooks**.
2. Name the token after your server, e.g. `Home server`.
3. Under *What this token can do*, tick **Read threads** and untick **Post messages** (mullet mode only reads).
4. Create it and **copy the token now**: it is shown only once.

### 3. (Recommended) Bring your personas over once

Mullet mode copies conversations, not the personas behind them: a persona's prompt is private working state that a read token deliberately cannot see.
Copied threads are matched to your local personas **by name**. Either bring them over once:

1. On whatiff.chat: **Import & Export → Export**. The download link arrives by email.
2. On your server: **Import & Export → Import** that file.

This also brings your memories.
Threads that came over in the export are recognised later, so mullet mode continues them instead of copying them twice.

Or simply create a persona here with the same name (say, `Vix`): every copied thread that was written with Vix picks it up straight away.
Either way you can do it before or after step 4; after step 4, the welcome screen tells you which names your threads are waiting for.

### 4. Paste the token at home

1. Sign in to your server. (A brand-new account is asked to create a persona first; the welcome screen also offers **Keep a copy of those threads here**, which goes straight to the next step.)
2. Open **Tools → Remote sync**.
3. Leave the server address as `https://api.whatiff.chat`, paste the token, and press **Save**.

Saving checks the token with whatiff.chat first and tells you plainly if it is mistyped, revoked, or cannot read threads.
The first sync starts straight away; the status shows **Syncing…** and then **In sync**, with how many threads and messages came over.
A big account comes over in rounds of 50 threads, one straight after another, until it is all here.

That is it. From now on, open the app at home and your threads are there.

## Day to day

| You want to… | Do this |
|---|---|
| See what you said on your phone | Open the app at home (or come back to its tab after a few minutes away). New messages arrive a few seconds later. |
| Continue a conversation at home | Open the copied thread and reply. |
| Get it now, without waiting | **Tools → Remote sync → Sync now**. |
| Pause it | Untick **Keep this server in sync** and save. |
| Stop for good | **Stop syncing and forget the token**, then revoke the token on whatiff.chat. Copies already here stay. |
| Not have a thread at home | Delete the copy. Sync will not bring it back. |

## What comes over, and what does not

| Comes over | Does not (yet) |
|---|---|
| Every thread, active and archived, with its name | Persona prompts and settings (bring them with an export, step 3) |
| Every message: who said it, what was said, and when, plus the thread's running summary | Memories (an export brings them; copied threads also build new ones here as you use them) |
| Which model and persona wrote each reply (as a label) | Attachments and images (their names are known, the files are not copied) |
| Renames on whatiff.chat, unless you renamed the copy | Tool calls and model reasoning |

Copies are one-way, from whatiff.chat to your server.

## Settings for operators

| Variable | Default | What it does |
|---|---|---|
| `REMOTE_SYNC_ENABLED` | `false` (`true` in Docker Compose) | Turns mullet mode on for this server. When off, the Tools tab does not appear. |
| `REMOTE_SYNC_INTERVAL` | `1h` | Background sync for copies nobody is looking at, on top of syncing when the app is used. Each run is spread randomly over up to a fifth of the interval later (an hour means every 60 to 72 minutes), so servers do not all call whatiff.chat at once. At least `1m`, or `off` to sync only when the app is used. |
| `REMOTE_SYNC_MAX_THREADS_PER_RUN` | `50` | Threads per round. A sync with more to do carries on with another round straight away. |
| `REMOTE_SYNC_ALLOW_LOCAL_NETWORK` | `false` | Allows `http://` and private addresses, for syncing from another What Iff server on your own network (or for development). Otherwise sync only connects to public `https` addresses. |

**Running your own models.** To continue copied threads on a local model, point What Iff at an OpenAI-compatible server (Ollama, LM Studio, vLLM):

```bash
ENV=local
LLM_BACKEND=local
LOCAL_LLM_BASE_URL=http://localhost:11434/v1   # Ollama's default
LOCAL_LLM_MODEL=qwen2.5:14b                     # whatever you have pulled
```

`LLM_BACKEND=local` is honoured only with `ENV` set to `local`, `development` or `test`, and serves every chat from that one model.
Alternatively, keep `LLM_BACKEND` at its default and give the server your own provider API keys (see `.env.example`).

## Privacy and security

- **The token can read every thread in your whatiff.chat account** (never persona prompts, scratchpads or memories).
  Treat it like a password: your server stores it encrypted, never shows it again (only its last four characters), and never logs it.
  Revoke it on whatiff.chat at any time and syncing stops.
- **Nothing flows back.** Mullet mode only reads from whatiff.chat. What you write at home stays at home.
- **Copying is not moving.** Your conversations stay on whatiff.chat too. To remove something from there, delete it there.
- **Your server only talks to public `https` addresses** for sync, checked on every connection, unless you allow your local network.
- **Copied threads come with whatiff.chat's running summary of them**, so the persona can pick up a long conversation without re-reading it.
  When there is no summary to bring (or a lot came over since it was written), your server summarizes the thread itself the next time you use it, with your local model or keys, not whatiff.chat.

## Troubleshooting

| What you see | What it means |
|---|---|
| No **Remote sync** tab under Tools | The server runs without `REMOTE_SYNC_ENABLED=true`. |
| "The token cannot read threads" | The token was made without **Read threads**. Create a new one with it ticked. |
| "The remote server did not accept the token" | Mistyped, or revoked on whatiff.chat. Paste a fresh one. |
| **Needs attention** | whatiff.chat refused the token during a sync, so syncing paused. Saving a working token resumes it. |
| **Will retry** | A sync hit a network or server problem. It tries again by itself. |
| A copied thread shows **Unassigned** | No persona here has its name yet. Create one with that name, or import an export (step 3), and it takes those threads. You can also pick a persona for the thread yourself. |
| Two threads with the same name, one ending "(from api.whatiff.chat)" | You continued it in both places; both continuations are kept. |
| Syncing from a server on your LAN fails | Set `REMOTE_SYNC_ALLOW_LOCAL_NETWORK=true` on the server doing the copying. |

## Known gaps in the beta

- Personas, memories and attachments need an export (above); a fuller read scope is planned.
- It is one-way. Sending what you write at home back to whatiff.chat is not supported.
- Copies are not marked as copies in the thread list yet, and a thread that split in two does not show the link between them yet.

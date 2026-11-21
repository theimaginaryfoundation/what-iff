# ADR 0x052: Voice replies and dictation

- **Status:** Proposed
- **Date:** 2026-11-21
- **Deciders:** What Iff maintainers
- **Issue:** none (one of the most requested features)
- **PR:** see the pull request that adds this file

## Context

People want to hear a personality's replies, and to talk instead of type.
That is two features with one shape:

- **Voice replies** (text to speech): an assistant message is read aloud, word for word, in a voice chosen for the personality.
- **Dictation** (speech to text): the person records a clip from the chat input, and its transcript lands in the composer.

Both sit on top of the message history, which stays text.
A reply is generated as text the way it is today, and speech is a rendering of that text.
A dictated message is text the person can read and edit before sending.

### What is ruled out

**Speech-to-speech and realtime voice models.**
They take audio or text and answer in generated audio, so the answer never exists as a message in the history, and the history is what every other feature (memory, jobs, relays, search, export) reads.

**Generative voice models used as text to speech.**
An earlier prototype tried asking a conversational audio model to read a reply out.
However it was prompted, it would not just read: it added a preamble, summarised, signed off, and often took the reply (already an assistant's text) as the *user's* message and answered it.
Voice replies need a dedicated speech synthesis model that reads its input verbatim.

**New speech-only vendors** (ElevenLabs, Cartesia, Deepgram and the like) are not added here.
This pass uses providers the app already connects to (ADR 0x027), with their existing keys.

### The market, November 2026

The earlier prototype was OpenAI-only, and it is a dead end:
OpenAI shuts down `tts-1`, `tts-1-hd` and `gpt-4o-mini-tts` on 2027-01-06, and names only `gpt-realtime-2.1-mini` as the replacement, which is a realtime conversational model and does not serve `/v1/audio/speech`.
OpenAI keeps speech *recognition*: `whisper-1` and the `gpt-4o-*-transcribe` models shut down on 2027-02-26, replaced by `gpt-transcribe`.

What the providers we support offer today:

| Provider | Speech synthesis (verbatim) | Style instruction | Voices | Transcription | Same key and host as chat |
| --- | --- | --- | --- | --- | --- |
| Google (Gemini API) | `gemini-3.8-flash-tts`, `gemini-3.8-flash-lite-tts` | Yes: a separate `style` field (`speech_metadata`); the text is treated strictly as a transcript | 30 prebuilt | `gemini-3.5-transcribe` (preview); any Gemini chat model takes audio | Yes (`v1beta`) |
| Qwen (Model Studio, international) | `qwen-audio-3.1-tts-flash`, `-plus`; `qwen3-tts-flash` | Yes: a separate `instruction` field (3.1 models) | System voices (count per model differs) | `qwen3-asr-flash` (through compatible-mode chat, audio as an input part) | Same key; synthesis is on the native DashScope API, not compatible mode |
| OpenAI | None after 2027-01-06 | n/a | n/a | `gpt-transcribe` (`/v1/audio/transcriptions`) | Yes |
| Mistral | `voxtral-mini-tts-2603`; Voxtral 3 TTS (preview) | No: inline emotion and pace tags only | Presets, and cloning from a reference clip | `voxtral-*-transcribe-3-0` (preview, `/v1/audio/transcriptions`) | Yes |
| OpenRouter | `/api/v1/audio/speech` routes Gemini, Voxtral and others | `instructions`, honoured by some models | Per model | `/api/v1/audio/transcriptions` | Yes |
| DeepSeek | `/v1/tools/tts` (new) | No | 2 | `/v1/tools/asr` (WAV or Ogg Opus only, 120 s) | Yes |
| Xiaomi MiMo | `mimo-v2.5-tts`, over chat completions | Yes, as a user message | 8 (Chinese and English) | `mimo-v2.5-asr` | Yes |
| Z.AI | International: none (`glm-tts` is China-only) | n/a | n/a | `glm-asr-2512` (30 s cap, `/audio/transcriptions`) | Yes |
| Anthropic | None | n/a | n/a | None | n/a |
| Ollama | `/v1/audio/speech` (English, macOS only so far, no style) | No | 4 | None | Yes |
| LM Studio | None | n/a | n/a | None | n/a |

Gemini 3.8 and Qwen Audio 3.1 are the only options with all three things a personality voice wants: verbatim reading, a choice of base voices, and a free-text style instruction kept apart from the text.
Most of the rest share OpenAI's audio routes (`/v1/audio/speech`, `/v1/audio/transcriptions`), so one client covers OpenRouter, Mistral and Z.AI transcription, Ollama, and self-hosted servers behind a Custom connection (Speaches, Kokoro-FastAPI, LocalAI); Mistral's synthesis differs only in its field names and answer.

Two facts about the app shape the design:

- **Replies reach the browser by polling.** A chat turn writes its text to the job as it streams (`draft_deltas`), and the client polls the job. There is no socket to push audio down.
- **A reply's saved text includes every tool round.** Text an agent writes before a tool call ("let me look that up") stays in the message, separated from the next round's text by a paragraph break. Only the last round is the answer.

## Decision

### 1. Two features, both optional

Voice replies and dictation are separate, and each is off until it is configured (decision 4).
Neither changes how a reply is generated, a message's text, or what any other feature reads.
An install with no speech models looks and behaves exactly as it does today: no play buttons, no microphone.

### 2. Speech models are models of their own kind

A model gains a **kind**: `chat` (every model today, and the default), `speech` (synthesis) or `transcription`.
A speech or transcription model is never offered for chat, subagents or the text utility roles, and a chat model is never offered as a voice or for dictation.

Provider listings say nothing useful about speech models: which voices they have, whether they take a style instruction, how much text one request takes.
So the app keeps a **known speech models** table in code, one entry per model id, giving:

| Field | Meaning |
| --- | --- |
| `kind` | `speech` or `transcription`. |
| `adapter` | Which client talks to it (decision 3). |
| `voices` | The base voices, each an id and a display name, and for speech models only. |
| `style` | Whether the model takes a free-text style instruction. |
| `max_input` | How much text one synthesis request may carry, or how long a clip one transcription may be. |
| `formats` | Audio formats it returns (speech) or accepts (transcription). |

A connection to a built-in provider lists the known speech models for that provider alongside its chat models, and they are enabled the same way.
The filter that drops OpenAI's `tts-` and `-transcribe` ids from listings stays, except for the transcription models this table knows; OpenAI's synthesis models are not offered at all, since they shut down weeks after this ships.
For OpenRouter, Ollama and Custom connections, whose model ids are open-ended, a speech or transcription model is added by id, with its kind and voices typed in (a Custom connection to a Kokoro server, for example, lists the voice ids the server has).

A speech model row stores the **voices it offers**: by default every voice the known table lists, narrowed or relabelled by whoever configures it (decision 4).
That list is what a personality picks from.

### 3. Speech adapters

Synthesis and transcription do not fit the utility call (`UtilityRequest` is text in, text or JSON out), so they get their own interfaces, the way image generation has `ImageGenerator`:

- `SpeechSynthesizer.Synthesize(ctx, SpeechRequest) (SpeechAudio, SpeechUsage, error)`: text, voice, optional style, preferred format; returns one complete audio file.
- `Transcriber.Transcribe(ctx, TranscriptionRequest) (string, TranscriptionUsage, error)`: one audio clip, its format, an optional language hint; returns text.

Adapters in this pass, chosen by the model's `adapter`:

| Adapter | Synthesis | Transcription | Serves |
| --- | --- | --- | --- |
| `openai_audio` | `POST {base}/audio/speech` (`instructions` sent only where `style` is true) | `POST {base}/audio/transcriptions` (multipart) | OpenAI (transcription only), Mistral and Z.AI transcription, OpenRouter, Ollama, Custom |
| `mistral` | `POST {base}/audio/speech` with `voice_id`, answering base64 JSON | (uses `openai_audio`) | Mistral synthesis |
| `gemini` | Native `v1beta` Interactions API, `speech_metadata.style` for the style, WAV out | The clip uploaded through the Files API, transcribed by `gemini-3.5-transcribe`, and the upload deleted | Google |
| `qwen` | Native DashScope `SpeechSynthesizer`, `instruction` for the style; the returned audio URL is fetched before returning | Compatible-mode chat completions with the clip as an `input_audio` part, `qwen3-asr-flash` | Qwen |

The Gemini and Qwen adapters derive their native roots from the connection's base URL the way their image generators already do, and use the connection's key through the same credential transport.
Gemini's native API takes its key as `x-goog-api-key`, so its speech paths (`/interactions`, `/files`, and the Files API upload under `/upload`) get path-scoped credential rules beside the one images use.
Each adapter retries a provider's transient failures as image generation does, and names the provider in a failure.

DeepSeek, Xiaomi MiMo and Z.AI synthesis are left for later (see Later); each is one more adapter.

### 4. Configuring voice

Voice gets a section in Settings beside **Background models** (ADR 0x027 §6), with two parts.

**Dictation model.**
Transcription is a new utility role, `transcription`, chosen in the same table, with the same rules: a choice is a model or Off, a role nobody chose is not set, and where the deployment supplies the keys the deployment chooses and accounts do not.
The picker offers only `transcription` models.
With the role off or not set, there is no microphone.

**Voices.**
Synthesis is not one role with one model: different personalities speak with different models and voices.
So this part lists the speech models enabled on the connections in scope, and for each one the voices it offers, which can be narrowed (hide voices nobody should pick) and relabelled.
Whoever chooses background models chooses these too: the account where it brings its own keys, the deployment where it supplies them.
A deployment's speech models are catalog models, like its chat models.
With no speech model enabled, personalities have no voice settings and nothing is read aloud.

The section says plainly that speech goes to the voice's provider, which need not be the chat's.

**Order and default.**
Speech models carry the same `subscription_tier` grade as chat models, and every voice picker lists voices by their model's grade, lowest first (models with no grade last), then by model and voice name, the order the chat model picker already uses (`modelTierRank`).
Where a voice is chosen for the first time, the picker preselects the first one.
So whoever configures voice decides what a person gets by just turning voice on: the cheapest model, graded lowest.
The grade only orders and labels; it does not restrict who may pick a voice.

Each speech model in a picker, the preview button and the autoplay settings have an extension outlet beside them for a cost hint; it is empty in this repository.

### 5. A personality's voice

A personality gains an optional voice, set and cleared on its own route (`PUT` and `DELETE /personality/{id}/voice`), so a personality save from an editor that knows nothing of voices never clears it:

| Field | Meaning |
| --- | --- |
| `voice_model_id` | A `speech` model. Empty means the personality has no voice. |
| `voice` | One of the voices that model offers. |
| `voice_style` | Free text, up to 500 characters: how this personality sounds ("warm, unhurried, a dry Scottish lilt; smiles through the jokes"). Sent as the model's style instruction; hidden when the model takes none. |
| `voice_autoplay` | Whether this personality's replies are read aloud as they arrive (decision 9). Off by default. |

The personality page gets a **Voice** section: model, voice, style, autoplay, and a **preview**.
The preview speaks a sample line in the unsaved settings, so the person hears a change before keeping it.
The sample is prefilled ("Hi, I'm {name}. This is how I'll sound when I read my replies to you.") and can be replaced with any text up to 300 characters.
A preview is `POST /speech/preview` with the model, voice, style and text; it is billed like any other synthesis (decision 11) and is not stored.

A voice whose model is disabled, deleted, or no longer offers that voice is unusable: the personality page says so, and the personality's replies have no play button until a voice is chosen again.
Nothing falls back to another voice silently, since a personality's voice is part of who it is.

### 6. What is spoken

The spoken text of a reply is its **final round**: the text written after the turn's last tool round, which is the answer.
Text from earlier rounds ("let me check the forecast") is narration of work, and is not read.
That holds for every way a reply is spoken, Listen included: reasoning, tool calls and narration are never read aloud.
A message records where its final round starts (`final_round_offset`, zero for a reply with no tool rounds), written when the reply is saved from the same boundary the draft buffer already marks.

That text is prepared for the ear before it is sent:

- Markdown is reduced to its words: emphasis, headings and list markers go; a link is read as its text.
- Fenced code blocks, tables and bare URLs are skipped.
- Emoji and HTML are dropped.
- Inline vocal tags a model understands (`<laugh>`, `[pace=slow]`) are left alone only if the personality's prompt puts them there; this pass adds none.

The prepared text is split into **segments** at sentence and paragraph boundaries: the first segment is one sentence, so speech can start quickly, and later segments are paragraphs up to the model's `max_input`.
Each segment is one synthesis request and one audio file.

### 7. Speech tracks

A reply's audio is a **speech track**: its segments, in order, in one voice.

| Field | Meaning |
| --- | --- |
| `chat_id` | The chat. |
| `job_id` | The turn that is writing the reply, while it runs. |
| `message_id` | The reply, once saved. |
| `voice_key` | The model, voice and style the track was made with. |
| `status` | `open` (segments still coming), `complete`, `failed`, or `stopped` (the meter refused, decision 11). |
| `autoplay` | Empty, `pending`, `played` or `skipped` (decision 9). |

Each **segment** records its sequence number, its text, its audio file in the file store, the format, its duration and its size.

A message has at most one track per `voice_key`: playing a reply again in the same voice replays the stored audio and calls no provider; playing it after the personality's voice changed makes a new track.
Segment audio is kept for 30 days after the track was last played, then deleted, and a later play synthesises it again.
Tracks and their audio also go when their message or chat is deleted: an hourly sweeper deletes the audio files and then the rows, rather than a database cascade that would drop the rows and strand the files.
They are not part of an account export (they can be made again from the text).

Synthesis runs on the server, one segment at a time, in a background worker per track, writing each segment as it finishes.
The client never sends text to synthesise; it asks for a message's track:

- `POST /speech/messages/{id}`: the reply's track in its personality's current voice, started if it does not exist.
- `GET /speech/tracks/{id}`: its status and its segments so far, each with its format and, where the container says, its duration.
- `GET /speech/tracks/{id}/segments/{seq}`: one segment's audio, privately cacheable since it never changes.

A worker holds a lease on the track, renewed per segment, so a track whose server went away mid-way is picked up by the next request for it.
A track that failed, or stopped when the meter refused, is reopened when asked for again, keeping the segments it has.

The client polls a track every second while it is `open`, and plays segments back to back as they arrive.

**Playback.**
One player for the whole app: starting a reply stops whatever was playing.
Segments are decoded and scheduled back to back with Web Audio, so there is no gap between them.
Leaving the thread stops playback, as does starting dictation.
Each assistant message whose chat has a usable voice gets a **Listen** button beside Copy, which becomes **Stop** while it plays.

### 8. Speaking while the reply streams

A reply that is read aloud as it arrives should start speaking as it starts appearing, not when it is done.
The turn already writes its text to the job as it streams, so the track follows the same text on the server, in the same process, instead of waiting for the saved message.

The catch is decision 6: only the final round is spoken, and while a round streams nothing says whether it is the last.
A round that ends in a tool call was narration.
So a round's text is held until it **commits**:

- the round reaches 200 characters (narration before a tool call is almost always one short sentence), or
- the round ends without a tool call, which makes it the final round.

The 200 characters are a debounce, not a minimum: a reply that finishes shorter ("Sure, done!") commits when it ends, and is spoken.

Nothing is synthesised before a round commits.
After that, its sentences are segmented and synthesised as they complete.
If a committed round still ends in a tool call (a long preamble, rare), its segments are withdrawn: the track drops them, a client playing one stops at the end of that segment, and the track starts again with the next round.
A saved reply's track holds only its final round, matching decision 6.

Holding to 200 characters costs a second or two of generation before speech starts; first sound is then that, plus one sentence of synthesis, plus at most one poll.
It is cheaper and simpler than synthesising every round speculatively and paying for narration nobody hears.

The streaming track is created when the turn starts (decision 9 says when), and the job carries its id, so the client watching the turn finds it with the polls it already makes.
Synthesis during the turn starts only once the track is claimed, which means a client is there to hear it.
An unclaimed track synthesises nothing while the turn runs; if it is claimed later, it is made from the saved reply like any other.

### 9. Autoplay

**Setting it.**
A personality's `voice_autoplay` is the default for its chats.
A chat can override it: `voice_autoplay` on the chat is empty (follow the personality), on, or off, set on `PUT /speech/chats/{id}/autoplay` (a PATCH couldn't tell "empty" from "not sent").
So a personality that never reads aloud can still have one thread that always does.
The chat's control is a speaker button beside the microphone in the composer ("Read replies aloud: Personality default (off) · Always · Never"), shown only when the personality has a voice.

**Which replies.**
Whether a reply should be read aloud is decided once, on the server, when its turn starts: if autoplay resolves on for the chat and the voice is usable, the turn creates the reply's track with `autoplay: pending`.
That covers every way a reply arrives: the person's own turns, agent jobs posting into the chat (ADR 0x039), and relay turns.
It does not depend on whether anyone is watching.

**Playing it once.**
A pending track is played at most once, by one client.
The client claims it (`POST /speech/tracks/{id}/claim`), which moves it from `pending` to `played` atomically and refuses anything else, and plays only if the claim succeeds.
A client claims only when it can play right then: the chat is open and in view, the tab is visible, audio is unlocked (below), dictation is not recording, and the person has not started another reply playing.
It looks for a pending track when it opens a chat (`GET /speech/chats/{id}/pending`, newest first), when one appears in the open chat (a turn or job starting, through the job's `speech_track_id`), and when its tab becomes visible again.
A pending track it can't play now (the tab hidden, dictation recording, another reply playing) is left pending, to be looked for again.

That gives:

| What happened | What plays |
| --- | --- |
| The person sends and watches | The reply, as it streams (decision 8). |
| The person sends, leaves, and comes back after the reply finished | The reply, when they come back. |
| A job posts into the open chat | Its reply, as it streams. |
| A job posted overnight; the person opens the chat in the morning | That reply, from its saved text. |
| The person comes back to the chat again later | Nothing: the track is `played`. |
| The chat is open on a laptop and a phone | Whichever claims first; the other does nothing. |
| Several replies are pending when the chat opens | The newest. The others are marked `skipped`, and keep their Listen buttons. |
| A pending reply is more than a day old | Nothing; it is marked `skipped`. |
| Autoplay is turned off for the chat | Its pending tracks are marked `skipped`. |

Autoplay state is separate from read status.
A reply is marked read when the chat loads, as today, whether or not it has been played; read status is never a reason to play or not play.

**Browsers' autoplay rules.**
Browsers block sound a page starts without a gesture, iOS most strictly.
The client unlocks one audio context on the person's first tap or click in the session (sending, opening a chat, the microphone).
When a pending track is found and audio is still locked (the app opened straight into a chat from a notification, say), the client does not claim it; the reply shows **Tap to listen**, and tapping claims and plays it.

### 10. Dictation

With a dictation model set, the composer gets a **microphone** button beside Send.

- Pressing it asks for microphone permission the first time, then records, showing a timer and a level meter.
- Pressing it again stops and transcribes; **Escape** cancels and discards the clip.
- The transcript is inserted at the caret, and the composer keeps focus. Dictation never sends: the person reads, edits, and sends as usual.
- Recording stops any reply that is playing, and nothing autoplays while it records.
- A clip is limited to the model's `max_input` or five minutes, whichever is shorter; recording stops at the limit and says so.

**Audio formats.**
Browsers record in different formats: WebM with Opus in Chromium and Firefox, MP4 with AAC in Safari.
The client records in the browser's own format when the dictation model's `formats` accept it, and otherwise captures 16 kHz mono WAV through Web Audio (about 10 MB for five minutes).
The server does no transcoding, so it needs no native audio tooling.

`POST /speech/transcriptions` takes the clip (multipart), its format, the chat it is for, and an optional language hint from the account's locale, and returns the text.
The clip is never stored or logged; only its duration is recorded.

A native shell has to declare microphone access for the web view to get it.
Where `getUserMedia` is not available, there is no microphone button.

### 11. Usage and limits

Speech is metered through the existing `metering.Meter`, with two new action types: `speech_synthesis` and `transcription`.

- **Check** comes before every synthesis request (each segment, and each preview) and every transcription.
- **Record** comes after each, with the model, the provider, the characters synthesised or the audio duration transcribed, and the provider's token counts where it reports them (Gemini and Qwen do).
- **Replaying stored audio is not a synthesis** and records nothing.

A refused check is the same `429 quota_exceeded` a chat turn gets.
A track stops at the refused segment (`stopped`), keeping what it has.
The client hands a refusal from any speech call to the gate the composer already consults, and while that gate reports the account blocked, Listen and the microphone are disabled and autoplay claims nothing.

Speech also emits OpenTelemetry metrics: characters synthesised, audio seconds transcribed, time to first segment, segment failures, and replays served from storage.

The self-hosted build's meter records nothing, as for chat: the provider bills the key's owner.

## Rollout

1. **Speech models and adapters** (decisions 2 and 3): model kind, the known speech models table, the three adapters, and listings that show speech models.
2. **Dictation** (decisions 4 and 10, and the transcription half of 11): the `transcription` role, the Voice section's dictation part, the microphone.
3. **Voices and Listen** (decisions 4–7, and the synthesis half of 11): offered voices, a personality's voice and preview, final-round offsets, speech tracks, the player and the Listen button.
4. **Autoplay and streaming** (decisions 8 and 9): turn-time tracks, claiming, the chat override.

Each step ships on its own; dictation is useful without voice replies, and Listen without autoplay.

## Consequences

- Voice works without OpenAI. A deployment can run voice replies on Gemini or Qwen, and dictation on any of Gemini, Qwen, OpenAI, Mistral or OpenRouter, using keys it already has.
- Models gain a kind, and every model picker filters on it. A picker that forgets to filter offers a voice as a chat model; the model list endpoints filter by kind by default for that reason.
- A reply's spoken text and its displayed text differ when it used tools: the narration before tool calls is shown but not read. That is deliberate, and visible: the Listen button reads from the answer, not from the top of the bubble.
- Turns in autoplay chats do a little more work at start (a track row) whether or not anyone listens; they synthesise only when someone does.
- The known speech models table has to be kept up to date by hand, as the voice catalogues and model ids change faster than chat models do. A model missing from it can still be added by id on an OpenAI-compatible connection.
- Gemini 3.8 TTS doubles in price on 2027-01-01. Gemini's 2.5 preview TTS models shut down on 2026-12-18 and are not offered.
- Qwen is moving international accounts from `dashscope-intl.aliyuncs.com` to workspace hosts; the Qwen adapter derives its native root from the connection's base URL, so a connection updated to the new host keeps working.

## Alternatives considered

- **The browser's own speech (Web Speech API).** Free and local, but every device has different voices, none of them is the personality's, and recognition sends audio to the browser vendor in some browsers and is missing in others. It may come back as an offline fallback (see Later).
- **Autoplay only what the client watched arrive.** Simple, and wrong: a reply that lands after the person left, or a job's reply, would never play.
- **Autoplay unread replies.** Read status changes as a chat loads, a backlog of job replies would all play in a row, and two devices would both play. Autoplay gets its own claimable state instead.
- **Synthesising every autoplay reply as it is written.** Pays for audio nobody hears when nobody is watching. Synthesis waits for a claim.
- **Pushing audio over a socket or server-sent events.** The app has no push channel; replies already come by polling, and segments ride the same polls. Worth revisiting if a socket arrives for other reasons.
- **Speaking the whole message, narration included.** "Let me check that. Okay, here's what I found." is not the answer, and reads badly aloud.
- **Synthesising rounds speculatively before they commit.** Faster to first sound by a second or so, at the cost of paying for narration that is then thrown away.
- **Synthesis as one utility role.** One model and one voice for everybody; personalities need their own.
- **Transcoding on the server (ffmpeg).** A native dependency in every deployment; capturing WAV in the browser covers the models that do not take compressed audio.
- **OpenAI `gpt-4o-mini-tts` in the meantime.** It shuts down about six weeks after this could ship, with no verbatim successor.

## Later

- **More synthesis adapters:** DeepSeek (two voices, no style), Xiaomi MiMo (synthesis over chat completions, Chinese and English voices), Z.AI if `glm-tts` reaches its international API, and Google Cloud Text-to-Speech, which needs a Cloud credential rather than a Gemini key.
- **Custom voices:** Gemini 3.8 voice design, Qwen voice design and cloning, Voxtral cloning. Cloning raises consent questions this ADR does not answer.
- **Hands-free voice mode:** send on silence, live captions over streaming transcription, and autoplay of the reply, for a conversation without the keyboard.
- **Sharing voices:** a personality's export and marketplace listing carry its voice as provider, model, voice and style, bound again on import where that model is available.
- **Two voices in one reply** (multi-speaker synthesis) for role-play.
- **Playback speed**, keyboard shortcuts for Listen and the microphone, and an offline fallback to the browser's own speech.

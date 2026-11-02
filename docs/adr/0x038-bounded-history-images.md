# ADR 0x038: Bounded history images and JPEG uploads

- **Status:** Accepted
- **Date:** 2026-11-02
- **Deciders:** What Iff maintainers
- **Issue:** [what-iff#419](https://github.com/theimaginaryfoundation/what-iff/issues/419)
- **PR:** [what-iff#420](https://github.com/theimaginaryfoundation/what-iff/pull/420); companion what-iff-private#233 (`GOMEMLIMIT`)

## Context

A customer generated more than ten images in one thread, and the prod API (a 1 GB Fargate task, about 896 MB of it for the API container) was OOM-killed during the next inference.

Every chat turn rebuilds its request from scratch.
`buildHistoryContext` replays up to 50 post-checkpoint messages plus carry-over turns, and for vision models `historyImagesForMessage` downloaded **every image attachment on them at full size** from the object store.
A generated image is a 1.5–3 MB PNG.
An uploaded photo was re-encoded by `NormalizeForUpload` as a PNG of up to 2048 px, about 5 MB, larger than the JPEG it came from.

The bytes are then encoded into the provider request: base64 (plus a `fmt.Sprintf` data URL on OpenAI), then the SDK's `json.Marshal`.
The openai-go and anthropic-sdk-go marshallers re-copy the output of every nested `MarshalJSON`, so marshalling a request needs roughly ten times its image bytes.
Measured for one request with 2 MB images, with the GC running normally:

| History images | Raw bytes | Peak heap, OpenAI Responses | Peak heap, Claude |
|---|---|---|---|
| 5 | 10 MB | ~190 MB | ~165 MB |
| 10 | 20 MB | ~320 MB | ~295 MB |
| 20 | 40 MB | ~710 MB | ~660 MB |

The same images are rendered again by the checkpoint summary and archival steps.
The Claude adapter also re-marshals every tool round's images within a turn.
Two such turns at once on one task exceed its memory.

## Decision

### 1. History turns send thumbnails

`historyImagesForMessage` resolves each image with `wantThumb=true`, so prior turns send the 256 px JPEG thumbnail that every upload and generated image already stores.
An attachment without one falls back to its original.
We reuse the thumbnail rather than adding a "context-sized" rendition, so each image keeps two stored copies, not three.
History images are there for continuity ("the dragon from before"), which a thumbnail carries.
The current turn's attachments are still sent at full size.

### 2. At most 5 history images per request

`selectHistoryImages` keeps the newest `maxHistoryImages` (5) image attachments across history, newest first, and then carry-over.
Older images are left out silently, the way a model without vision already sees history.
Past a handful of images, continuity adds little and every extra image is re-sent on every turn.

### 3. Uploads are normalized to JPEG

`NormalizeForUpload` now re-encodes opaque images as JPEG at quality 90, which is about 1 MB for a 2048 px photo, and returns the new content type.
Images with any transparent pixel stay PNG, because JPEG has no alpha channel and flattening a sticker or logo onto a colour would change it.
Re-encoding still strips EXIF and other metadata, which the Buzz relay's `PrepareMedia` relies on.
Existing PNG uploads are not converted.

### 4. GOMEMLIMIT in deployed environments (private repo)

The API container gets `GOMEMLIMIT` at 80% of its share of the task.
That share is the task memory minus the OTel sidecar's 128 MB, so the limit is 716 MiB in prod and 307 MiB in dev.
Without it the Go GC lets the heap grow to twice the live size (`GOGC=100`) before collecting, so a short spike can cross the kernel's limit while most of the heap is garbage.
With it the GC collects more often as the heap approaches the limit.
It is a soft target: memory that is actually live still gets the task killed, which is why the first three decisions are the real fix.

## Alternatives considered

- **A third rendition at about 1024 px for model context.** It would show detail that a 256 px thumbnail loses, but it is another object per image to generate, store, back-fill and delete.
- **A byte budget instead of an image count.** It is more precise, but thumbnails are already small and bounded, so a count is simpler to reason about and to explain.
- **A placeholder line for each omitted image.** It would tell the model an image existed. But it changes cached history text as images age out, and bracketed notes in history risk being imitated in replies.
- **Streaming the request body, or bypassing the SDK marshaller.** This would remove the 10× marshal amplification for every request, but it would mean owning the wire format of each vendor SDK.
- **Keeping PNG for lossless uploads.** Providers downscale images before the model sees them, and the gallery shows photos, for which JPEG at quality 90 is indistinguishable.

## Consequences

- The peak heap for a request with 5 history images drops from about 190 MB to about 3 MB, with a 0.05 MB body.
- Fine detail in older images (small text in a screenshot from earlier turns) is no longer visible to the model; the user can re-attach the image.
- Images beyond the newest 5 are invisible to the model, though still in the gallery and `find_context`.
- New uploads are stored as `.jpg` / `image/jpeg` unless they are transparent; clients already handle both types.
- Fewer image tokens are sent per turn, so cost and latency also fall for image-heavy threads.

## Not done yet

- Within a single turn, images that `generate_image` produced are still sent back at full size, and the Claude adapter re-sends them on every tool round (up to 4 images × 10 rounds).
- Requests are still marshalled with roughly 10× amplification, so the current turn's own full-size attachments remain the dominant cost.
- Nothing back-fills thumbnails for old attachments that lack one; those fall back to the original (bounded by the cap).
- The unused `file_attachments.file_content` column is still in the schema and still loaded by `WithFileAttachments()`.

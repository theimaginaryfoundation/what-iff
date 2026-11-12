# ADR 0x047: Image markup and reference-image edits

- **Status:** Accepted
- **Date:** 2026-11-11
- **Deciders:** What Iff maintainers
- **Issue:** #481 (markup), #208 (reference images, first half)
- **PR:** see the pull request that adds this file

## Context

Image iteration was all prompt.
"Change the thing on the left" is ambiguous, and the agent had no way to hand an existing image to the image model: `generate_image` was text-to-image only.
The only reference-image path was OpenAI's edit endpoint, which portrait and expression generation use with one image and no mask.

The file workspace (ADR 0x043) gives every image a panel beside the thread.
That panel is the natural place to point at part of an image and say what should change there, or to draw a rough sketch for the agent to build an image from.

The image vendors differ:
- OpenAI's `images/edits` takes up to 16 input images and a mask: a PNG the size of the first image whose fully transparent pixels mark the area to change.
- Gemini's `generateContent` takes images inline with the text and edits them well from instructions, but has no mask parameter.
- z.ai and Qwen are left out for now.

## Decision

### 1. The agent stays in the loop

Marking up an image does not call the image model.
It adds the marks to the next message as attachments, and the person asks for the change in their own words ("I've marked the sky, can you make it a sunset?").
The agent then calls `generate_image`.
Nothing in the app starts an agent turn from outside the chat, and this keeps one path for metering, safety and provenance.

### 2. One tool: `generate_image` takes reference images and a mask

To avoid tool sprawl there is no separate edit tool; `generate_image` gains two optional arguments:
- `reference_images`: up to 3 images, each `{ "image": <name or id>, "role": base | markup | sketch | reference }`.
  A bare string is accepted as a name with the role `reference`.
- `mask_image`: the name of a mask for the base image, white where it may change and black where it must not.
  A mask without an explicit base makes the first reference the base.
  A reference with role `mask` is taken as `mask_image`: agents reach for it first (found in testing), so it is accepted rather than refused, and an unknown role's error says where masks go.

Names resolve the way the model sees images: images `generate_image` made earlier in the same turn, then the conversation's context files (ADR 0x043 §4) by name or workspace path, then the user's own files by id.
An unknown name is refused with the image names that are in the conversation, so the model can correct itself.
A mask without its image is refused with an error that says to pass the image as the base, or to ask the user to attach it; the mask's description names its image and says how to pass both.
The image need not be on the same message: it is often in the conversation already, so the composer does not insist on it.
The result lists every input as it was sent (`inputs`, by file name and role, base first and mask last) and names the mask (`mask_image`), so the agent can check from the receipt that the request was understood.
Inputs must be PNG, JPEG or WebP (sniffed from the bytes) and at most 20 MB.
Without `aspect_ratio`, an edit keeps the base image's shape.

### 3. Roles turn into instructions, so markup is never drawn

The provider layer writes the text that says what each input image is, before the agent's prompt:
- **base**: the image to edit; keep subject, style, lighting and framing unless asked.
- **markup**: a copy of the image with the user's marks; the marks say where and what to change and are never reproduced.
- **sketch**: follow its composition and shapes, not its lines or colours.
- **reference**: subject, character or style to carry over.

The tool description gives the agent the same conventions.

### 4. One mask format, translated per vendor

Masks are white-on-black images, which the agent can look at and understand.
`provider.ImageEditor` is an optional interface on `ImageGenerator`, and each editing vendor translates the mask:
- **OpenAI** stretches the mask to the base's size and turns white into transparent, then sends every input image with it.
  It sends no `input_fidelity`: the current image model refuses the parameter, and keeps what is outside the mask without it.
- **Gemini** gets a *guide image* instead: the base with the masked area tinted magenta at 55%, plus a sentence saying the tint only marks the area.

The translation happens once per request (`ImageEditor.PrepareEdit`), before the quota check and before the images are requested in parallel.
A mask that marks nothing, or cannot be decoded, is refused there with a message the agent can act on, and costs nothing.

Both were checked live on a photo (sky masked, a tree circled in markup): each changed only the marked area and drew neither the tint nor the circle.

### 5. No cross-vendor fallback

A request with reference images or a mask goes to the vendor the user chose under Settings → Image generation, and nowhere else.
Moving it to another vendor could cost more than the user chose, or send their images to a company they did not pick.
When the chosen vendor cannot edit, or has no key for the caller, the tool refuses with a message telling the agent to say so and how to fix it; text-only generation still works, and keeps its existing fallback to a vendor that has a key.
The markup surface warns up front, from the same preference (`imageEditingUnavailableReason`), so the person knows before drawing.

### 6. Mark up in the file workspace

Images opened from a thread get **Mark up** beside Edit.
It swaps the viewer for a markup surface over the image:
- **Area to change**: a brush that paints a see-through magenta highlight, which becomes the mask.
- **Pen**: coloured strokes for arrows, circles and notes, which become the markup copy.
- Size, pen colours, Undo (also Ctrl/Cmd+Z) and Clear.
- **Add to message** and Cancel; leaving with a drawing asks first, like unsaved edits.

Strokes are kept as paths in the image's own pixels, so the panel can resize without losing them, and they are drawn at full resolution on export.
Pointer events cover mouse, pen and touch (`touch-action: none` on the canvas).
Markup is offered on phones too, unlike Edit: drawing with a finger is the point.
It is offered only where there is a thread to add to (the chat-hosted workspace), not in the gallery.

Add to message uploads up to two new files into the thread, in the gallery's `image_masks` folder (they matter for a turn or two, so they collect out of the way), and attaches them with a reference copy of the original (`POST /image-gallery/{id}/reference`, as the gallery picker does: a file belongs to one message, and the original is usually on one already):
- `<name>-mask`, when the area brush was used, described as "Mask for <name>: white marks the area the user wants changed, black the area to keep."
- `<name>-markup`, when the pen was used, described as "<name> with the user's markup drawn on it: the marks show where and what to change, and are instructions, not content."

The reference copy is left out when the message already holds the original, or a reference copy of it from an earlier markup.
The upload is all or nothing: when one file fails, the ones already made are deleted, so trying again does not leave duplicates in the thread.

The descriptions reach the agent in the attachment label it already sees, so it knows what each file is without a naming convention.
Uploads accept a `description` field for this.
Over the thread (tablet, phone) the panel closes afterwards, so the message with its attachments is in view.

The composer's attachment hand-off became a queue: it held one file, so the second of several added in the same tick replaced the first.

### 7. Sketch from a blank page

**New canvas** opens the same surface over a blank white page (1024 px square) with a black pen.
It is in the right-hand panel's Files tab, beside Upload and From gallery, where the other file and image actions are, and in the composer's + menu.
Both are disabled until the chat has a thread, since the sketch is uploaded into it.
It is not in the gallery: starting there would mean switching back to the chat to ask for anything.
Add to message attaches `sketch-<time>` (filed in `sketches`), described as a rough sketch to base an image on.

### 8. The original is never touched

Markup only creates new files; the image it was drawn on is attached as it is.
The derived files are ordinary thread uploads in the gallery's `image_masks` folder, which the user can see and delete; sketches go to `sketches`.

## Alternatives considered

- **Filerobot's Annotate tab** (already in the image editor) for the marks.
  Its annotations flatten into the image on save, which would overwrite the original, and it cannot export the brush as a separate mask.
- **A direct "redraw selection" action** that skips the agent.
  Nothing else starts agent work from outside the chat, and marking up then asking in the thread is natural and sufficient.
- **A separate `edit_image` tool.**
  Tool sprawl; the model would have to choose between two tools for one job.
- **Falling back to a vendor that can edit** when the chosen one cannot.
  Rejected (§5): cost and data the user did not choose.
- **A prompt box on the markup surface** that fills the composer.
  The composer is where the person already writes; a second box splits the request in two.
- **Sending the mask to the agent in OpenAI's alpha form.**
  A transparent PNG reads as nothing to a vision model; white-on-black is legible, and only the adapter needs the alpha form.

## Consequences

- Any image the agent can see can be edited or used as a reference, including one it made earlier in the same turn.
- Edits cost the same as generations at the chosen quality, metered against the vendor that ran them (`path: tool_edit`).
- A user whose image vendor is z.ai or Qwen can draw markup but the agent cannot act on it; both the surface and the tool say so.
- Opaque uploads are normalised to JPEG (what-iff#419), so masks and markup copies are stored as JPEG.
  The mask is read by brightness with a 50% threshold, so compression at the edges does not matter.
- Masks and markup copies collect in the gallery's `image_masks` folder, sketches in `sketches`.

## Not done yet

- z.ai and Qwen edits (Qwen's image models take input images on the same endpoint; z.ai's support is unknown).
- Using the personality image as the reference for the first expression in personality creation (#208's second half).
- Shape tools (rectangle, ellipse, arrow); the pen covers them roughly.
- Editing a mask or markup after adding it; the person marks up again.
- Keyboard-only drawing: the tools are operable from the keyboard, the canvas itself is not.

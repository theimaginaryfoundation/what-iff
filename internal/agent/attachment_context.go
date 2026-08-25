package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/filechunker"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
)

const (
	inlineDocumentMaxTokens    = 8000
	largeDocumentPreviewChunks = 5
)

type attachmentDocContext struct {
	label   string
	inline  string
	preview string
	tooBig  bool
}

// buildFullAttachmentContext renders developer-facing attachment guidance for the
// current user turn, inlining small text documents and previewing the first chunks
// of larger ones.
func (b *messageContextBuilder) buildFullAttachmentContext(
	ctx context.Context,
	userID, chatID uuid.UUID,
	attachments []*models.FileAttachment,
) string {
	if len(attachments) == 0 {
		return ""
	}

	var images []string
	var docs []attachmentDocContext
	for _, a := range attachments {
		if a == nil {
			continue
		}
		label := attachmentLabel(a)
		if strings.HasPrefix(a.FileType, models.ImageMIMEPrefix) {
			images = append(images, label)
			continue
		}
		docs = append(docs, b.resolveDocumentAttachmentContext(ctx, userID, chatID, a, label))
	}

	if len(images) == 0 && len(docs) == 0 {
		return ""
	}

	var out strings.Builder
	out.WriteString("The user attached the following file(s) to this message:")
	for _, l := range images {
		fmt.Fprintf(&out, "\n- %s [image]", l)
	}
	for _, d := range docs {
		fmt.Fprintf(&out, "\n- %s [document]", d.label)
	}
	if len(images) > 0 {
		out.WriteString("\n\nImage attachments are already visible to you inline in this message.")
	}
	for _, d := range docs {
		if d.inline != "" {
			fmt.Fprintf(&out, "\n\nFull contents of %s:\n%s", d.label, d.inline)
		} else if d.preview != "" {
			fmt.Fprintf(&out, "\n\nPreview of %s (file is large; use find_context with mode=\"fetch\" for additional sections):\n%s", d.label, d.preview)
		} else if d.tooBig {
			fmt.Fprintf(&out, "\n\n%s is attached but too large to inline. Call find_context with mode=\"fetch\" and the exact file name %q.", d.label, docSearchName(d.label))
		}
	}
	if len(docs) > 0 {
		out.WriteString("\n\nFor document attachments without inlined text above, prefer find_context over guessing when the user asks about file contents.")
	}
	return out.String()
}

func (b *messageContextBuilder) resolveDocumentAttachmentContext(
	ctx context.Context,
	userID, chatID uuid.UUID,
	att *models.FileAttachment,
	label string,
) attachmentDocContext {
	out := attachmentDocContext{label: label}
	if !filechunker.IsTextType(att.FileType) && !filechunker.IsTextFileByExtension(att.Name) {
		return out
	}

	text, ok := storage.ResolveAttachmentTextContent(ctx, b.telemetry.Logger, b.fileStore, userID, att)
	if !ok {
		if b.ds != nil {
			if chunks, err := b.ds.ListFileChunksForAttachment(ctx, att.ID, largeDocumentPreviewChunks); err == nil && len(chunks) > 0 {
				out.preview = formatAttachmentChunkPreview(chunks)
				return out
			}
		}
		out.tooBig = true
		return out
	}

	tokens, err := b.tokenCounter.CountTokens(text)
	if err == nil && tokens <= inlineDocumentMaxTokens {
		out.inline = text
		return out
	}

	if b.ds != nil {
		if chunks, chunkErr := b.ds.ListFileChunksForAttachment(ctx, att.ID, largeDocumentPreviewChunks); chunkErr == nil && len(chunks) > 0 {
			out.preview = formatAttachmentChunkPreview(chunks)
			return out
		}
	}

	if err != nil || tokens > inlineDocumentMaxTokens {
		out.tooBig = true
		return out
	}
	out.inline = text
	return out
}

func attachmentLabel(a *models.FileAttachment) string {
	label := strings.TrimSpace(a.Name)
	if label == "" {
		label = "(unnamed file)"
	}
	if ft := strings.TrimSpace(a.FileType); ft != "" {
		label += " (" + ft + ")"
	}
	if a.Description != nil {
		if d := strings.TrimSpace(*a.Description); d != "" {
			label += " — " + d
		}
	}
	return label
}

func docSearchName(label string) string {
	if idx := strings.Index(label, " ("); idx > 0 {
		return label[:idx]
	}
	return label
}

func formatAttachmentChunkPreview(chunks []datastore.FileChunkResult) string {
	var b strings.Builder
	for _, c := range chunks {
		name := strings.TrimSpace(c.FileName)
		if name == "" {
			name = "document"
		}
		fmt.Fprintf(&b, "--- %s (chunk %d) ---\n%s\n\n", name, c.Sequence+1, strings.TrimSpace(c.Content))
	}
	return strings.TrimSpace(b.String())
}

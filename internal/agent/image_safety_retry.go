package agent

import (
	"context"
	"strings"

	"go.uber.org/zap"

	"github.com/theimaginaryfoundation/what-iff/internal/agent/provider"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/modeltypes"
)

// retryWithoutImagesOnRefusal runs call, one model request of the agent loop. When the provider
// refuses it on safety grounds and the adapter can strip the images it was sending, it strips
// them and runs call once more.
//
// Images are the likeliest trigger: a picture a vendor drew moments ago (generate_image results
// are fed back to the model) or one the user attached can be flagged by a different vendor's
// filter, and the text around it is usually fine. Retrying the one refused request keeps the
// tools that already ran from running again, which a whole-turn retry would not.
//
// The count of images left out accumulates on chatCtx (withholdImages) so the reply's context
// breakdown can say so. A refusal with no images to strip, or a second refusal, is returned as before and handled
// by the turn's existing safety-violation path.
func (a *Agent) retryWithoutImagesOnRefusal(ctx context.Context, chatCtx *chatContext, adapter provider.AgentAdapter, call func() error) error {
	err := call()
	if err == nil {
		return nil
	}
	violation, refused := provider.IsSafetyViolationError(err)
	if !refused {
		return err
	}
	stripper, ok := adapter.(provider.ImageStripper)
	if !ok {
		return err
	}
	withheld := stripper.StripImages()
	if withheld == 0 {
		return err
	}
	fields := []zap.Field{zap.Int("images_withheld", withheld), zap.String("provider", string(violation.Provider)), zap.String("provider_code", violation.ProviderCode)}
	if chatCtx != nil && chatCtx.chat != nil {
		fields = append(fields, zap.String("chat_id", chatCtx.chat.ID.String()))
	}
	a.logger.Warn("provider refused a request with images on safety grounds; retrying without them", fields...)
	chatCtx.withholdImages(withheld, modeltypes.ImagesWithheldSafetyRefusal)
	return call()
}

// withholdImages records n images this turn's model did not see. The first reason recorded
// stands: a model that cannot see images never sends any to be refused.
func (c *chatContext) withholdImages(n int, reason string) {
	if c == nil || n <= 0 {
		return
	}
	c.imagesWithheld += n
	if c.imagesWithheldReason == "" {
		c.imagesWithheldReason = reason
	}
}

// countImageAttachments counts the image attachments on a message.
func countImageAttachments(attachments []*models.FileAttachment) int {
	n := 0
	for _, att := range attachments {
		if att != nil && strings.HasPrefix(att.FileType, models.ImageMIMEPrefix) {
			n++
		}
	}
	return n
}

package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
)

// recordImageGenerationUsage, when non-nil, records billable usage for one
// generated image via the tool path. It is nil in builds without a metering
// implementation (the open-source build), so nothing is recorded there; a linked
// implementation sets it in an init(). The core reports only what was generated —
// the quality tier ("low"|"medium"|"high") and the image engine — and the
// recorder prices it.
var recordImageGenerationUsage func(ctx context.Context, ds *datastore.Datastore, userID uuid.UUID, quality string, engine string, chatID string, imageIndex int)

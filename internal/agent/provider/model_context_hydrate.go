package provider

import (
	"context"
	"fmt"
)

// HydrateUserMessageImages loads raw bytes for each UserMessageImage that has a FileID
// but empty RawBytes, using fetch (e.g. S3). OpenAI user_data files are not re-downloadable
// from the Files API; do not use OpenAI file content here.
func (m *ModelContext) HydrateUserMessageImages(ctx context.Context, fetch func(context.Context, string) ([]byte, error)) error {
	if m == nil || fetch == nil {
		return nil
	}
	for i := range m.Segments {
		if m.Segments[i].Kind != SegmentKindUserMessage {
			continue
		}
		imgs := m.Segments[i].UserImages
		for j := range imgs {
			if len(imgs[j].RawBytes) > 0 || imgs[j].FileID == "" {
				continue
			}
			b, err := fetch(ctx, imgs[j].FileID)
			if err != nil {
				return fmt.Errorf("load image file %q: %w", imgs[j].FileID, err)
			}
			imgs[j].RawBytes = b
		}
		m.Segments[i].UserImages = imgs
	}
	return nil
}

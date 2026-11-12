package tools

import (
	"context"
	"sync"
)

// A tool can show the model a picture with its result, such as a diagram's preview (ADR 0x049 §8),
// without making it a file of the reply: it adds the picture to the call's context
// (AddResultImage), and the agent loop sends what was added after the tool result, to models that
// take images.

// ResultImage is one picture shown with a tool result.
type ResultImage struct {
	Data      []byte
	MediaType string
}

// ResultImages collects the pictures added during one tool call.
type ResultImages struct {
	mu     sync.Mutex
	images []ResultImage
}

type resultImagesKey struct{}

// WithResultImages returns a context whose tool call can add pictures, and what collects them.
func WithResultImages(ctx context.Context) (context.Context, *ResultImages) {
	sink := &ResultImages{}
	return context.WithValue(ctx, resultImagesKey{}, sink), sink
}

// AddResultImage shows the model data with the result of the tool call ctx belongs to. Without a
// collector (a caller that shows no pictures) it does nothing.
func AddResultImage(ctx context.Context, data []byte, mediaType string) {
	sink, _ := ctx.Value(resultImagesKey{}).(*ResultImages)
	if sink == nil || len(data) == 0 {
		return
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.images = append(sink.images, ResultImage{Data: data, MediaType: mediaType})
}

// Images is what was added, in order.
func (s *ResultImages) Images() []ResultImage {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ResultImage(nil), s.images...)
}

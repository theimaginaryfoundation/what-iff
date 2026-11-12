package filecomments

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/textanchor"
)

// Changes made for someone else than the comments panel (ADR 0x046): an agent's comment tool reads
// a file's threads with the text they are on, changes them and writes them back, and places a new
// thread by quoting the text it is about rather than by offsets.

// ErrQuoteNotFound means a quote is not in the file's text; ErrQuoteAmbiguous that it is there
// more than once and no occurrence says which.
var (
	ErrQuoteNotFound  = errors.New("the quoted text is not in the file")
	ErrQuoteAmbiguous = errors.New("the quoted text is in the file more than once")
)

// Current is a file's threads as a change sees them: placed on Text, the file's text now (empty
// for an image, whose threads are all on the whole file).
type Current struct {
	File    *models.FileAttachment
	Text    string
	Image   bool
	Threads []models.FileCommentThread
}

// Update reads the threads of the file id is a row of, changes them with change and writes the
// result, again once when the file or its comments changed in between.
func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, change func(*Current) ([]models.FileCommentThread, error)) (*models.FileComments, error) {
	for attempt := 0; ; attempt++ {
		got, err := s.Read(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		file, err := s.store.GetFileAttachment(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		cur := &Current{File: file, Image: isImage(file), Threads: got.Threads}
		if !cur.Image {
			if cur.Text, err = s.text(ctx, userID, file); err != nil {
				return nil, err
			}
		}
		threads, err := change(cur)
		if err != nil {
			return nil, err
		}
		out, err := s.write(ctx, userID, id, threads, got.ContentVersion, got.Revision, false)
		if (errors.Is(err, ErrRevision) || errors.Is(err, ErrStaleContent)) && attempt == 0 {
			continue
		}
		return out, err
	}
}

// AnchorOn is the range of quote in text: its only place, or with occurrence n > 0 its nth.
func AnchorOn(text, quote string, occurrence int) (*models.FileCommentRange, error) {
	found := textanchor.FindAll(textanchor.Units(text), quote)
	switch {
	case len(found) == 0:
		return nil, ErrQuoteNotFound
	case occurrence > len(found):
		return nil, fmt.Errorf("%w: it is there %d times", ErrQuoteNotFound, len(found))
	case occurrence > 0:
		r := found[occurrence-1]
		return &models.FileCommentRange{Start: r.Start, End: r.End}, nil
	case len(found) > 1:
		return nil, fmt.Errorf("%w (%d times)", ErrQuoteAmbiguous, len(found))
	}
	return &models.FileCommentRange{Start: found[0].Start, End: found[0].End}, nil
}

// Lines is the first and last line of the text a range is on (1-based).
func Lines(text string, r models.FileCommentRange) (first, last int) {
	units := textanchor.Units(text)
	end := r.End
	if end > r.Start {
		end--
	}
	return textanchor.LineAt(units, r.Start), textanchor.LineAt(units, end)
}

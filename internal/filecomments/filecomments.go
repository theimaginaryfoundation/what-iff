// Package filecomments keeps the comments on text, code and image files (ADR 0x045). Those formats
// have no place for comments, so a file's threads are stored beside it. On text they are on ranges
// of it, kept with the text their anchors are placed against: whoever changes the file (the user's
// editor, an agent's file tools), the next read carries the anchors to the new text (textanchor),
// so they stay on the words they were on, and a thread none of whose words are left becomes a
// comment on the whole file. On an image every thread is on the whole file, for now.
package filecomments

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/storage"
	"github.com/theimaginaryfoundation/what-iff/internal/textanchor"
)

// MaxTextBytes is the largest file comments are kept for: the baseline is stored with them.
const MaxTextBytes = 4 << 20

var (
	// ErrStaleContent means the anchors were placed on a version of the file that is no longer
	// its content: the caller re-reads the file and its comments.
	ErrStaleContent = errors.New("the file changed since the comments were placed on it")
	// ErrRevision means the comments changed since the caller read them.
	ErrRevision = datastore.ErrFileCommentsRevision
	// ErrNotText means the file is neither UTF-8 text of at most MaxTextBytes nor an image, which
	// comments can be placed on.
	ErrNotText = errors.New("comments can be placed only on images and on UTF-8 text files of at most 4 MB")
)

// Store is what the service needs of the datastore.
type Store interface {
	GetFileAttachment(ctx context.Context, userID, id uuid.UUID) (*models.FileAttachment, error)
	CommentFileID(ctx context.Context, userID, id uuid.UUID) (uuid.UUID, error)
	GetFileCommentSet(ctx context.Context, userID, fileID uuid.UUID) (*models.FileCommentSet, error)
	PutFileCommentSet(ctx context.Context, userID, fileID uuid.UUID, set models.FileCommentSet, expectRevision int) (int, error)
}

// Service reads and writes file comments.
type Service struct {
	store Store
	files storage.FileStore
}

// New returns a Service.
func New(store Store, files storage.FileStore) *Service {
	return &Service{store: store, files: files}
}

// contentKey names the content a row holds: its stored object, which every save replaces. A row
// stored before keys were recorded is named by itself and its version.
func contentKey(file *models.FileAttachment) string {
	if file.S3Key != "" {
		return file.S3Key
	}
	return fmt.Sprintf("row:%s:%d", file.ID, file.ContentVersion)
}

// isImage reports whether the file is an image, whose threads are on the whole file.
//
// Regions of an image (a box drawn on it) are the expected next anchor. They would be a separate
// field of the thread, in fractions of the image's width and height so they do not depend on its
// size, and an edit that changes the image's geometry (a crop, a rotation) would move them or put
// them on the whole file; the baseline key already tells a read that the image changed.
func isImage(file *models.FileAttachment) bool {
	return strings.HasPrefix(strings.ToLower(file.FileType), "image/")
}

// onFile puts every thread on the whole file: an image's threads (see isImage).
func onFile(threads []models.FileCommentThread) []models.FileCommentThread {
	out := make([]models.FileCommentThread, len(threads))
	for i, t := range threads {
		if t.Comments == nil {
			t.Comments = []models.FileComment{}
		}
		t.Anchor = nil
		t.Quote = ""
		out[i] = t
	}
	return out
}

// Read is the comments of the file id is a row of (its original, for a chat copy: ADR 0x045),
// placed on the content id holds. When that is not the content they were placed on, their anchors
// are carried across and the result stored, so the next read is direct.
func (s *Service) Read(ctx context.Context, userID, id uuid.UUID) (*models.FileComments, error) {
	for attempt := 0; ; attempt++ {
		file, err := s.store.GetFileAttachment(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		key, err := s.store.CommentFileID(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		set, err := s.store.GetFileCommentSet(ctx, userID, key)
		if err != nil {
			return nil, err
		}
		if set == nil {
			return &models.FileComments{Threads: []models.FileCommentThread{}, ContentVersion: file.ContentVersion}, nil
		}
		// An image's threads are on the whole file, which an edit does not move.
		if set.BaselineKey == contentKey(file) || isImage(file) {
			return &models.FileComments{Threads: set.Threads, ContentVersion: file.ContentVersion, Revision: set.Revision}, nil
		}
		text, err := s.text(ctx, userID, file)
		if err != nil {
			return nil, err
		}
		threads := Carry(set.Threads, set.BaselineText, text)
		revision, err := s.store.PutFileCommentSet(ctx, userID, key, models.FileCommentSet{
			Threads: threads, BaselineText: text, BaselineKey: contentKey(file),
		}, set.Revision)
		// Another read carried them first: read what it stored.
		if errors.Is(err, ErrRevision) && attempt == 0 {
			continue
		}
		if err != nil {
			return nil, err
		}
		return &models.FileComments{Threads: threads, ContentVersion: file.ContentVersion, Revision: revision}, nil
	}
}

// Write stores the threads of the file id is a row of, placed on the content of id at
// contentVersion, which must be what it holds now, over the comments at revision (0 for a file
// with none yet).
func (s *Service) Write(ctx context.Context, userID, id uuid.UUID, threads []models.FileCommentThread, contentVersion, revision int) (*models.FileComments, error) {
	file, err := s.store.GetFileAttachment(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	if file.ContentVersion != contentVersion {
		return nil, ErrStaleContent
	}
	key, err := s.store.CommentFileID(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	var text string
	if isImage(file) {
		threads = onFile(threads)
	} else {
		if text, err = s.text(ctx, userID, file); err != nil {
			return nil, err
		}
		threads = Place(threads, text)
	}
	next, err := s.store.PutFileCommentSet(ctx, userID, key, models.FileCommentSet{
		Threads: threads, BaselineText: text, BaselineKey: contentKey(file),
	}, revision)
	if err != nil {
		return nil, err
	}
	return &models.FileComments{Threads: threads, ContentVersion: file.ContentVersion, Revision: next}, nil
}

func (s *Service) text(ctx context.Context, userID uuid.UUID, file *models.FileAttachment) (string, error) {
	data, err := storage.ReadAttachment(ctx, s.files, storage.AttachmentObject{
		UserID: userID, ID: file.ID, S3Key: file.S3Key, Name: file.Name, FileType: file.FileType,
		ChatID: file.ChatID, PersonalityID: file.PersonalityID,
	})
	if err != nil {
		return "", err
	}
	if len(data) > MaxTextBytes || !utf8.Valid(data) {
		return "", ErrNotText
	}
	return string(data), nil
}

// Place checks each thread's anchor against text and quotes the text it is on; an anchor outside
// the text puts its thread on the whole file.
func Place(threads []models.FileCommentThread, text string) []models.FileCommentThread {
	units := textanchor.Units(text)
	out := make([]models.FileCommentThread, len(threads))
	for i, t := range threads {
		if t.Comments == nil {
			t.Comments = []models.FileComment{}
		}
		if a := t.Anchor; a != nil {
			if a.Start < 0 || a.End > len(units) || a.Start >= a.End {
				t.Anchor = nil
			} else {
				t.Quote = textanchor.Slice(units, textanchor.Range{Start: a.Start, End: a.End})
			}
		}
		out[i] = t
	}
	return out
}

// Carry moves each thread's anchor from the text it was placed on to the new text: kept on its
// words, or on the whole file, with the quote it had, once none are left.
func Carry(threads []models.FileCommentThread, oldText, newText string) []models.FileCommentThread {
	ranges := make([]textanchor.Range, 0, len(threads))
	for _, t := range threads {
		if t.Anchor != nil {
			ranges = append(ranges, textanchor.Range{Start: t.Anchor.Start, End: t.Anchor.End})
		}
	}
	moved := textanchor.Remap(oldText, newText, ranges)
	units := textanchor.Units(newText)
	out := make([]models.FileCommentThread, len(threads))
	next := 0
	for i, t := range threads {
		if t.Anchor != nil {
			if r := moved[next]; r != nil {
				t.Anchor = &models.FileCommentRange{Start: r.Start, End: r.End}
				t.Quote = textanchor.Slice(units, *r)
			} else {
				t.Anchor = nil
			}
			next++
		}
		out[i] = t
	}
	return out
}

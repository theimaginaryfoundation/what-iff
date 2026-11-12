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
	"time"
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
	// errTextTooLarge is the ErrNotText of a file over MaxTextBytes, which can still hold comments
	// on the whole file (Write).
	errTextTooLarge = fmt.Errorf("%w (it is over 4 MB)", ErrNotText)
	// ErrContentMissing means the file's stored object could not be found: a save or an agent edit
	// replaced it after the row was read, or it is gone. Its text is not known, so no anchor is moved.
	ErrContentMissing = errors.New("the file's content could not be found")
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

// contentKey names the content a row holds: its stored object, which every save replaces (with a
// new key). A row stored before keys were recorded is named by the object it is read from, which is
// also the key its chat copies were given, so reading through either does not carry the comments.
func contentKey(userID uuid.UUID, file *models.FileAttachment) string {
	if file.S3Key != "" {
		return file.S3Key
	}
	return storage.FileKeyForAttachment(userID, file.ID, file.Name, file.FileType, file.ChatID, file.PersonalityID)
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

// onWholeFile reports whether no thread is on a range of the text.
func onWholeFile(threads []models.FileCommentThread) bool {
	for _, t := range threads {
		if t.Anchor != nil {
			return false
		}
	}
	return true
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
		if set.BaselineKey == contentKey(userID, file) || isImage(file) {
			return &models.FileComments{Threads: set.Threads, ContentVersion: file.ContentVersion, Revision: set.Revision}, nil
		}
		text, err := s.text(ctx, userID, file)
		// The row moved on to a new object while this read: read it again.
		if errors.Is(err, ErrContentMissing) && attempt < 2 {
			continue
		}
		// The file is no longer text comments can be placed on (it grew past MaxTextBytes, or is
		// not UTF-8): its comments stay readable, on the whole file, and nothing is stored.
		if errors.Is(err, ErrNotText) {
			return &models.FileComments{Threads: onFile(set.Threads), ContentVersion: file.ContentVersion, Revision: set.Revision}, nil
		}
		if err != nil {
			return nil, err
		}
		threads := Carry(set.Threads, set.BaselineText, text)
		revision, err := s.store.PutFileCommentSet(ctx, userID, key, models.FileCommentSet{
			Threads: threads, BaselineText: text, BaselineKey: contentKey(userID, file),
		}, set.Revision)
		// Another read carried them first: read what it stored.
		if errors.Is(err, ErrRevision) && attempt < 2 {
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
//
// The threads come from the client, so authorship is set here (keepAuthorship).
func (s *Service) Write(ctx context.Context, userID, id uuid.UUID, threads []models.FileCommentThread, contentVersion, revision int) (*models.FileComments, error) {
	return s.write(ctx, userID, id, threads, contentVersion, revision, true)
}

// write is Write; fromClient sets authorship from what is stored rather than from the threads
// given, which a server-side change (an agent's comment tool, Update) sets itself.
func (s *Service) write(ctx context.Context, userID, id uuid.UUID, threads []models.FileCommentThread, contentVersion, revision int, fromClient bool) (*models.FileComments, error) {
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
	if fromClient {
		prev, err := s.store.GetFileCommentSet(ctx, userID, key)
		if err != nil {
			return nil, err
		}
		threads = keepAuthorship(threads, prev, userID, time.Now().UTC())
	}
	var text string
	if isImage(file) {
		threads = onFile(threads)
	} else {
		text, err = s.text(ctx, userID, file)
		switch {
		case errors.Is(err, errTextTooLarge) && onWholeFile(threads):
			// A file past MaxTextBytes keeps comments on the whole file (Read shows them so): no
			// text is needed to place those, and the stored key still marks the baseline.
			threads, text = onFile(threads), ""
		case err != nil:
			return nil, s.missingIfStale(ctx, userID, id, contentVersion, err)
		default:
			threads = Place(threads, text)
		}
	}
	next, err := s.store.PutFileCommentSet(ctx, userID, key, models.FileCommentSet{
		Threads: threads, BaselineText: text, BaselineKey: contentKey(userID, file),
	}, revision)
	if err != nil {
		return nil, err
	}
	return &models.FileComments{Threads: threads, ContentVersion: file.ContentVersion, Revision: next}, nil
}

// text is the file's content, read from the object the row names. A row naming an object that is
// not there is ErrContentMissing, not empty text: a carry onto "" would put every anchor nowhere
// and store that as the baseline.
func (s *Service) text(ctx context.Context, userID uuid.UUID, file *models.FileAttachment) (string, error) {
	if file.SizeBytes != nil && *file.SizeBytes > MaxTextBytes {
		return "", errTextTooLarge
	}
	var data []byte
	var err error
	if file.S3Key != "" {
		// Only the object the row names: another key could hold an older version.
		data, err = s.files.DownloadFile(ctx, file.S3Key)
	} else {
		data, err = storage.ReadAttachment(ctx, s.files, storage.AttachmentObject{
			UserID: userID, ID: file.ID, S3Key: file.S3Key, Name: file.Name, FileType: file.FileType,
			ChatID: file.ChatID, PersonalityID: file.PersonalityID,
		})
	}
	if err != nil {
		return "", err
	}
	// DownloadFile is (nil, nil) for a missing object and non-nil for an empty one. A row from
	// before keys were recorded cannot tell the two apart (ReadAttachment is nil for both), and
	// has no object a save could swap away mid-read: it is read as empty, as it always was.
	if data == nil && file.S3Key != "" {
		return "", ErrContentMissing
	}
	if len(data) > MaxTextBytes {
		return "", errTextTooLarge
	}
	if !utf8.Valid(data) {
		return "", ErrNotText
	}
	return string(data), nil
}

// missingIfStale turns a write's ErrContentMissing into ErrStaleContent when the row has moved on
// from contentVersion since it was read, so the caller re-reads rather than failing.
func (s *Service) missingIfStale(ctx context.Context, userID, id uuid.UUID, contentVersion int, err error) error {
	if !errors.Is(err, ErrContentMissing) {
		return err
	}
	if now, gerr := s.store.GetFileAttachment(ctx, userID, id); gerr == nil && now.ContentVersion != contentVersion {
		return ErrStaleContent
	}
	return err
}

// keepAuthorship sets who wrote each comment from what the server knows, not from the client: a
// comment already stored keeps its author and time (an agent's stays the agent's, and its text
// too: the user may remove it, not reword it), and a new one is the user's, dated now when the
// client's time is missing or ahead of the server's.
func keepAuthorship(threads []models.FileCommentThread, prev *models.FileCommentSet, userID uuid.UUID, now time.Time) []models.FileCommentThread {
	stored := map[string]models.FileComment{}
	if prev != nil {
		for _, t := range prev.Threads {
			for _, c := range t.Comments {
				stored[c.ID] = c
			}
		}
	}
	me := "user:" + userID.String()
	out := make([]models.FileCommentThread, len(threads))
	for i, t := range threads {
		comments := make([]models.FileComment, len(t.Comments))
		for j, c := range t.Comments {
			if was, ok := stored[c.ID]; ok {
				c.AuthorID, c.AuthorName, c.CreatedAt = was.AuthorID, was.AuthorName, was.CreatedAt
				if was.AuthorID != me {
					c.Text, c.Mentions = was.Text, was.Mentions
				}
			} else {
				c.AuthorID = me
				if c.CreatedAt.IsZero() || c.CreatedAt.After(now.Add(time.Minute)) {
					c.CreatedAt = now
				}
			}
			comments[j] = c
		}
		t.Comments = comments
		out[i] = t
	}
	return out
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

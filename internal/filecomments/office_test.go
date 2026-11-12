package filecomments

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/filesave"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
)

// appendFormat "comments" by appending to the bytes.
type appendFormat struct{}

func (appendFormat) List(data []byte) ([]officecomments.Thread, error) {
	return []officecomments.Thread{{ID: "t", On: string(data)}}, nil
}
func (appendFormat) Add(data []byte, at officecomments.Anchor, _ officecomments.Comment) ([]byte, string, error) {
	return append(append([]byte(nil), data...), at.Quote...), "t", nil
}
func (appendFormat) Reply(data []byte, _ string, _ officecomments.Comment) ([]byte, error) {
	return data, nil
}
func (appendFormat) Resolve(data []byte, _ string, _ bool) ([]byte, error) { return data, nil }

// memSaver saves into memFiles, refusing the first conflicts saves as if someone else saved.
type memSaver struct {
	store     *fakeStore
	files     memFiles
	conflicts int
	saved     []filesave.Request
}

func (s *memSaver) Replace(_ context.Context, _ uuid.UUID, req filesave.Request) (*models.FileAttachment, error) {
	if s.conflicts > 0 {
		s.conflicts--
		// Someone else saved: the file is at a new version with new content.
		s.store.file.ContentVersion++
		s.files[s.store.file.S3Key] = append(s.files[s.store.file.S3Key], '!')
		return nil, &filesave.ConflictError{Current: s.store.file.ContentVersion}
	}
	if *req.IfMatch != s.store.file.ContentVersion {
		return nil, &filesave.ConflictError{Current: s.store.file.ContentVersion}
	}
	s.saved = append(s.saved, req)
	s.files[s.store.file.S3Key] = req.Content
	s.store.file.ContentVersion++
	return s.store.file, nil
}

func TestOfficeChangeSavesANewVersionOverTheOneRead(t *testing.T) {
	files := memFiles{"k1": []byte("doc")}
	store := &fakeStore{file: &models.FileAttachment{ID: uuid.New(), S3Key: "k1", Name: "Brief.DOCX", ContentVersion: 1}}
	saver := &memSaver{store: store, files: files, conflicts: 1}
	office := NewOffice(store, files, saver, map[string]officecomments.Format{".docx": appendFormat{}})
	ctx := context.Background()
	chatID := uuid.New()

	if office.FormatOf(&models.FileAttachment{Name: "notes.md"}) != nil || office.FormatOf(store.file) == nil {
		t.Fatal("format by extension, any case")
	}
	id, err := office.Change(ctx, uuid.New(), &chatID, store.file.ID, func(f officecomments.Format, data []byte) ([]byte, string, error) {
		return f.Add(data, officecomments.Anchor{Quote: "+c"}, officecomments.Comment{})
	})
	if err != nil || id != "t" {
		t.Fatalf("change: %q %v", id, err)
	}
	if string(files["k1"]) != "doc!+c" || len(saver.saved) != 1 || *saver.saved[0].IfMatch != 2 || *saver.saved[0].ChatID != chatID {
		t.Fatalf("saved %q after %+v", files["k1"], saver.saved)
	}
	threads, err := office.List(ctx, uuid.New(), store.file.ID)
	if err != nil || threads[0].On != "doc!+c" {
		t.Fatalf("list %+v %v", threads, err)
	}

	// Two conflicts in a row: the file is changing; nothing is saved.
	saver.conflicts = 2
	if _, err := office.Change(ctx, uuid.New(), nil, store.file.ID, func(f officecomments.Format, data []byte) ([]byte, string, error) {
		return data, "t", nil
	}); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("conflicts: %v", err)
	}

	store.file.Name = "notes.md"
	if _, err := office.List(ctx, uuid.New(), store.file.ID); !errors.Is(err, ErrNotText) {
		t.Fatalf("not office: %v", err)
	}
}

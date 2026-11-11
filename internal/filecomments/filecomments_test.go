package filecomments

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

type memFiles map[string][]byte

func (m memFiles) UploadFile(_ context.Context, key string, content []byte, _ string) error {
	m[key] = content
	return nil
}
func (m memFiles) DownloadFile(_ context.Context, key string) ([]byte, error) { return m[key], nil }
func (m memFiles) DeleteFile(_ context.Context, key string) error {
	delete(m, key)
	return nil
}

type fakeStore struct {
	file *models.FileAttachment
	set  *models.FileCommentSet
	puts int
}

func (f *fakeStore) GetFileAttachment(context.Context, uuid.UUID, uuid.UUID) (*models.FileAttachment, error) {
	copied := *f.file
	return &copied, nil
}
func (f *fakeStore) GetFileCommentSet(context.Context, uuid.UUID, uuid.UUID) (*models.FileCommentSet, error) {
	if f.set == nil {
		return nil, nil
	}
	copied := *f.set
	return &copied, nil
}
func (f *fakeStore) PutFileCommentSet(_ context.Context, _, _ uuid.UUID, set models.FileCommentSet, expect int) (int, error) {
	current := 0
	if f.set != nil {
		current = f.set.Revision
	}
	if current != expect {
		return 0, ErrRevision
	}
	f.puts++
	set.Revision = current + 1
	f.set = &set
	return set.Revision, nil
}

func setup(text string) (*Service, *fakeStore, memFiles) {
	files := memFiles{"k1": []byte(text)}
	store := &fakeStore{file: &models.FileAttachment{ID: uuid.New(), S3Key: "k1", Name: "notes.txt", FileType: "text/plain", ContentVersion: 1}}
	return New(store, files), store, files
}

func thread(id string, start, end int) models.FileCommentThread {
	return models.FileCommentThread{
		ID:       id,
		Anchor:   &models.FileCommentRange{Start: start, End: end},
		Comments: []models.FileComment{{ID: id, AuthorID: "user:u1", AuthorName: "Pat", Text: "Hmm"}},
	}
}

const line = "This is an inline comment in a text file"

func TestWritePlacesThreadsOnTheCurrentTextAndQuotesThem(t *testing.T) {
	svc, store, _ := setup(line)
	ctx := context.Background()
	got, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{thread("a", 11, 25), thread("bad", 30, 999)}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Revision != 1 || got.Threads[0].Quote != "inline comment" || got.Threads[1].Anchor != nil {
		t.Fatalf("got %+v", got)
	}
	if store.set.BaselineText != line || store.set.BaselineVersion != 1 {
		t.Fatalf("baseline %+v", store.set)
	}
	// A write against a version that is no longer the file's, or a stale revision, is refused.
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, nil, 2, 1); !errors.Is(err, ErrStaleContent) {
		t.Fatalf("stale content: %v", err)
	}
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, nil, 1, 0); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale revision: %v", err)
	}
}

func TestReadCarriesAnchorsToTextChangedElsewhere(t *testing.T) {
	svc, store, files := setup(line)
	ctx := context.Background()
	if _, err := svc.Write(ctx, uuid.New(), store.file.ID, []models.FileCommentThread{thread("whole", 0, len(line)), thread("words", 11, 25)}, 1, 0); err != nil {
		t.Fatal(err)
	}
	// An agent rewrites the file.
	files["k2"] = []byte("I've updated this inline comment in Go source code")
	store.file.S3Key, store.file.ContentVersion = "k2", 2

	got, err := svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ContentVersion != 2 || got.Revision != 2 {
		t.Fatalf("got %+v", got)
	}
	if got.Threads[0].Quote != "inline comment in" || got.Threads[1].Quote != "inline comment" {
		t.Fatalf("quotes %q %q", got.Threads[0].Quote, got.Threads[1].Quote)
	}
	// The carried anchors are stored, so the next read is direct.
	puts := store.puts
	if _, err := svc.Read(ctx, uuid.New(), store.file.ID); err != nil || store.puts != puts {
		t.Fatalf("second read wrote again: %v", err)
	}

	// All of a thread's words gone: it is on the whole file, with the quote it had.
	files["k3"] = []byte("Something else entirely")
	store.file.S3Key, store.file.ContentVersion = "k3", 3
	got, err = svc.Read(ctx, uuid.New(), store.file.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Threads[1].Anchor != nil || got.Threads[1].Quote != "inline comment" {
		t.Fatalf("detached %+v", got.Threads[1])
	}
}

func TestReadOfAFileWithNoCommentsAndOfBinary(t *testing.T) {
	svc, store, files := setup(line)
	got, err := svc.Read(context.Background(), uuid.New(), store.file.ID)
	if err != nil || len(got.Threads) != 0 || got.Revision != 0 || got.ContentVersion != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	files["k1"] = []byte{0xff, 0xfe, 0x00}
	if _, err := svc.Write(context.Background(), uuid.New(), store.file.ID, nil, 1, 0); !errors.Is(err, ErrNotText) {
		t.Fatalf("binary: %v", err)
	}
}

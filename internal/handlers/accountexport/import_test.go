package accountexport

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestReadZipEntryDistinguishesMissingAndOversizedEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("large.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	if _, found, err := readZipEntry(zr, "missing.json", 5); err != nil || found {
		t.Fatalf("missing entry = found:%v err:%v, want found:false err:nil", found, err)
	}
	if _, found, err := readZipEntry(zr, "large.json", 5); !found || err == nil {
		t.Fatalf("oversized entry = found:%v err:%v, want found:true non-nil error", found, err)
	}
}

func TestValidateImportArchiveRejectsTooManyEntries(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i <= maxImportEntries; i++ {
		w, err := zw.Create("entry-" + strings.Repeat("x", i%3))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}

	if err := validateImportArchive(zr); err == nil {
		t.Fatal("validateImportArchive() error = nil, want entry limit error")
	}
}

func TestRemapMemoryArchiveRewritesChatAndPersonalityReferences(t *testing.T) {
	sourceChat, destinationChat := uuid.New(), uuid.New()
	sourcePersonality, destinationPersonality := uuid.New(), uuid.New()
	sourceMemory := uuid.New()
	createdAt := time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC)
	record, err := json.Marshal(models.MemoryRecord{
		ID:        sourceMemory,
		Content:   "remember this",
		ChatID:    &sourceChat,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	var source bytes.Buffer
	zw := zip.NewWriter(&source)
	for name, content := range map[string][]byte{
		"chat.json": append(record, '\n'),
		"personality-" + sourcePersonality.String() + ".json": []byte("{}\n"),
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(source.Bytes()), int64(source.Len()))
	if err != nil {
		t.Fatal(err)
	}

	targetUserID := uuid.New()
	remapped, err := remapMemoryArchive(zr, targetUserID,
		map[uuid.UUID]uuid.UUID{sourceChat: destinationChat},
		map[uuid.UUID]uuid.UUID{sourcePersonality: destinationPersonality},
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := zip.NewReader(bytes.NewReader(remapped), int64(len(remapped)))
	if err != nil {
		t.Fatal(err)
	}
	chatData, found, err := readZipEntry(out, "chat.json", 1024)
	if err != nil || !found {
		t.Fatalf("chat.json found:%v err:%v", found, err)
	}
	var got models.MemoryRecord
	if err := json.Unmarshal(bytes.TrimSpace(chatData), &got); err != nil {
		t.Fatal(err)
	}
	if got.ChatID == nil || *got.ChatID != destinationChat {
		t.Fatalf("chat ID = %v, want %v", got.ChatID, destinationChat)
	}
	if want := uuid.NewSHA1(targetUserID, sourceMemory[:]); got.ID != want {
		t.Fatalf("memory ID = %v, want target-scoped %v", got.ID, want)
	}
	if _, found, err := readZipEntry(out, "personality-"+destinationPersonality.String()+".json", 1024); err != nil || !found {
		t.Fatalf("remapped personality entry found:%v err:%v", found, err)
	}
}

func TestRemapMemoryArchiveRejectsDuplicateEntryNames(t *testing.T) {
	var source bytes.Buffer
	zw := zip.NewWriter(&source)
	for _, content := range []string{"first\n", "second\n"} {
		w, err := zw.Create("chat.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(source.Bytes()), int64(source.Len()))
	if err != nil {
		t.Fatal(err)
	}

	_, err = remapMemoryArchive(zr, uuid.New(), nil, nil)
	if !errors.Is(err, errDuplicateMemoryArchiveEntry) {
		t.Fatalf("remapMemoryArchive() error = %v, want duplicate entry error", err)
	}
}

func TestProgressForAccountImportIncludesCountsAndTerminalResult(t *testing.T) {
	result := models.AccountImportResult{
		Conversations: models.ImportResult{Imported: 2, Skipped: 3},
		Memories:      models.MemoryImportResult{ImportedCount: 5, DuplicateCount: 7},
		Personalities: models.SectionImportCounts{Created: 11, Skipped: 13},
		Warnings:      []string{"memory embeddings unavailable"},
	}

	progress := progressForAccountImport("complete", "Account import complete.", result)
	if progress.Result == nil || progress.Result.Memories.ImportedCount != 5 {
		t.Fatalf("terminal result = %#v, want preserved result", progress.Result)
	}
	if got, want := progress.Counts["conversations_imported"], 2; got != want {
		t.Fatalf("conversations_imported = %d, want %d", got, want)
	}
	if got, want := progress.Counts["memories_skipped"], 7; got != want {
		t.Fatalf("memories_skipped = %d, want %d", got, want)
	}
}

package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestParsePreviewSources(t *testing.T) {
	t.Parallel()

	a, b := uuid.New(), uuid.New()
	got, err := ParsePreviewSources(fmt.Sprintf(" %s:3, %s:1,%s:9", a, b, a))
	if err != nil || len(got) != 2 || got[0] != (PreviewSource{ID: a, ContentVersion: 3}) || got[1] != (PreviewSource{ID: b, ContentVersion: 1}) {
		t.Fatalf("got %+v, %v; want a@3 and b@1, the repeat dropped", got, err)
	}
	if got, err := ParsePreviewSources(""); err != nil || got != nil {
		t.Fatalf("empty: got %+v, %v", got, err)
	}
	for _, bad := range []string{"nope", a.String(), a.String() + ":0", a.String() + ":x", "x:1"} {
		if _, err := ParsePreviewSources(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	many := make([]string, MaxPreviewSources+5)
	for i := range many {
		many[i] = uuid.NewString() + ":1"
	}
	if got, _ := ParsePreviewSources(strings.Join(many, ",")); len(got) != MaxPreviewSources {
		t.Fatalf("kept %d, want %d", len(got), MaxPreviewSources)
	}
}

func TestComparePreviewSources(t *testing.T) {
	t.Parallel()

	same, changed, gone := uuid.New(), uuid.New(), uuid.New()
	rows := map[uuid.UUID]*models.FileAttachment{
		same:    {Name: "a.css", Folder: "site", ContentVersion: 1},
		changed: {Name: "app.js", Folder: "site/js", ContentVersion: 4},
	}
	lookup := func(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.FileAttachment, error) {
		out := map[uuid.UUID]*models.FileAttachment{}
		for _, id := range ids {
			if r, ok := rows[id]; ok {
				out[id] = r
			}
		}
		return out, nil
	}
	got := ComparePreviewSources(context.Background(), []PreviewSource{{same, 1}, {changed, 3}, {gone, 2}}, lookup)
	if !got.Stale() || len(got.Changed) != 1 || got.Changed[0] != "site/js/app.js" || got.Gone != 1 {
		t.Fatalf("got %+v; want site/js/app.js changed and one gone", got)
	}
	if ComparePreviewSources(context.Background(), []PreviewSource{{same, 1}}, lookup).Stale() {
		t.Fatal("unchanged sources counted as stale")
	}
	failing := func(context.Context, []uuid.UUID) (map[uuid.UUID]*models.FileAttachment, error) {
		return nil, errors.New("db down")
	}
	if ComparePreviewSources(context.Background(), []PreviewSource{{changed, 3}}, failing).Stale() {
		t.Fatal("a failed lookup counted as a change")
	}
}

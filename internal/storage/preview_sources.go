package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// A web page's picture (ADR 0x050 §8) shows more than the page's own file: the stylesheets,
// scripts and images it loaded too. Its sources are those files at the versions they were when it
// was drawn, kept beside it, so a reader can tell a picture that a change to one of them left
// behind. A picture with no sources stored (a diagram's, or a page drawn on its own) is current
// whenever it is of the file's current object.

// PreviewSource is a file a preview was drawn from, at the content_version it had.
type PreviewSource struct {
	ID             uuid.UUID `json:"id"`
	ContentVersion int       `json:"content_version"`
}

// MaxPreviewSources is how many sources a preview keeps; a page that loads more is checked against
// the first this many.
const MaxPreviewSources = 100

func fileKeyForPreviewSources(objectKey string) string {
	return previewBase(objectKey) + ".sources.json"
}

// ParsePreviewSources reads sources written as `id:version` pairs separated by commas (the form the
// preview endpoint takes them in); an empty string is none. More than MaxPreviewSources keeps the first.
func ParsePreviewSources(s string) ([]PreviewSource, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []PreviewSource
	seen := map[uuid.UUID]bool{}
	for _, part := range strings.Split(s, ",") {
		idText, versionText, ok := strings.Cut(strings.TrimSpace(part), ":")
		id, err := uuid.Parse(idText)
		if !ok || err != nil {
			return nil, fmt.Errorf("source %q is not id:version", part)
		}
		version, err := strconv.Atoi(versionText)
		if err != nil || version < 1 {
			return nil, fmt.Errorf("source %q has no version", part)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		if len(out) < MaxPreviewSources {
			out = append(out, PreviewSource{ID: id, ContentVersion: version})
		}
	}
	return out, nil
}

// PutPreviewSources stores the sources of the object's preview, after PutPreview (which removes any
// it had).
func PutPreviewSources(ctx context.Context, store FileStore, objectKey string, sources []PreviewSource) error {
	if len(sources) == 0 {
		return nil
	}
	data, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	return store.UploadFile(ctx, fileKeyForPreviewSources(objectKey), data, "application/json")
}

// PreviewSources are the sources stored with the object's preview; none (nil, nil) when it has none.
func PreviewSources(ctx context.Context, store FileStore, objectKey string) ([]PreviewSource, error) {
	data, err := store.DownloadFile(ctx, fileKeyForPreviewSources(objectKey))
	if err != nil || len(data) == 0 {
		// None (a diagram, a page drawn on its own) is the common case: the picture is then
		// checked by its own file alone.
		return nil, err
	}
	var sources []PreviewSource
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

// PreviewChanges is how a preview's sources differ from the files now.
type PreviewChanges struct {
	// Changed are the paths (folder/name) of sources with a newer version.
	Changed []string
	// Gone counts sources that no longer exist.
	Gone int
}

// Stale reports whether anything the picture shows has changed.
func (c PreviewChanges) Stale() bool { return len(c.Changed) > 0 || c.Gone > 0 }

// ComparePreviewSources compares sources with the files as they are now. lookup gives a file, nil
// for one that no longer exists, or an error, after which that source is not counted either way.
func ComparePreviewSources(ctx context.Context, sources []PreviewSource, lookup func(context.Context, uuid.UUID) (*models.FileAttachment, error)) PreviewChanges {
	var out PreviewChanges
	for _, s := range sources {
		att, err := lookup(ctx, s.ID)
		switch {
		case err != nil:
		case att == nil:
			out.Gone++
		case att.ContentVersion != s.ContentVersion:
			out.Changed = append(out.Changed, path.Join(att.Folder, att.Name))
		}
	}
	return out
}

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

// previewSourcesFile is what is kept beside a picture: its sources, and the tag of the picture they
// belong to, so a picture another browser stored meanwhile is never judged by these.
type previewSourcesFile struct {
	Tag     string          `json:"tag"`
	Sources []PreviewSource `json:"sources"`
}

// PutPreviewSources stores the sources of the object's preview, whose PNG is png, after PutPreview
// (which removes any it had).
func PutPreviewSources(ctx context.Context, store FileStore, objectKey string, png []byte, sources []PreviewSource) error {
	if len(sources) == 0 {
		return nil
	}
	data, err := json.Marshal(previewSourcesFile{Tag: PreviewTagOf(png), Sources: sources})
	if err != nil {
		return err
	}
	return store.UploadFile(ctx, fileKeyForPreviewSources(objectKey), data, "application/json")
}

// PreviewSources are the sources stored with the object's preview; none (nil, nil) when it has
// none, or when the ones stored belong to another picture than the one there now (two browsers
// storing at once, each writing its picture and then its sources).
func PreviewSources(ctx context.Context, store FileStore, objectKey string) ([]PreviewSource, error) {
	data, err := store.DownloadFile(ctx, fileKeyForPreviewSources(objectKey))
	if err != nil || len(data) == 0 {
		// None (a diagram, a page drawn on its own) is the common case: the picture is then
		// checked by its own file alone.
		return nil, err
	}
	var kept previewSourcesFile
	if err := json.Unmarshal(data, &kept); err != nil {
		return nil, err
	}
	tag, err := PreviewTag(ctx, store, objectKey)
	if err != nil {
		return nil, err
	}
	if tag == "" || tag != kept.Tag {
		return nil, nil
	}
	return kept.Sources, nil
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

// FileVersions gives the user's files among ids (their name, folder and content version) in one
// lookup; one that no longer exists is absent. datastore.FileAttachmentVersions is one.
type FileVersions func(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.FileAttachment, error)

// ComparePreviewSources compares sources with the files as they are now. A lookup that fails says
// nothing either way: no change is reported.
func ComparePreviewSources(ctx context.Context, sources []PreviewSource, versions FileVersions) PreviewChanges {
	var out PreviewChanges
	if len(sources) == 0 {
		return out
	}
	ids := make([]uuid.UUID, len(sources))
	for i, s := range sources {
		ids[i] = s.ID
	}
	now, err := versions(ctx, ids)
	if err != nil {
		return out
	}
	for _, s := range sources {
		att := now[s.ID]
		switch {
		case att == nil:
			out.Gone++
		case att.ContentVersion != s.ContentVersion:
			out.Changed = append(out.Changed, path.Join(att.Folder, att.Name))
		}
	}
	return out
}

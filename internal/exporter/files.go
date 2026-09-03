package exporter

import "sort"

// FileObject is a pointer to one binary object in the user's S3 prefix. The bundle carries the
// listing and checksums, never the bytes — files are delivered via per-object presigned URLs.
type FileObject struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type,omitempty"`
	ETag        string `json:"etag,omitempty"`
}

// FilesManifest is the files/manifest.json payload: what raw files exist and how to think about them.
type FilesManifest struct {
	Prefix     string       `json:"prefix"`
	Count      int          `json:"count"`
	TotalBytes int64        `json:"total_bytes"`
	Note       string       `json:"note"`
	Objects    []FileObject `json:"objects"`
}

const filesManifestFile = "files/manifest.json"

const filesManifestNote = "Inventory of the binary files (images/attachments) stored for this " +
	"account. The file bytes are not included in this export; this is a record of what exists. " +
	"Direct file download is planned as a follow-up."

// WriteFilesManifest projects the file listing to files/manifest.json. Objects are sorted by key so
// the manifest is deterministic across exports.
func WriteFilesManifest(tree Tree, prefix string, objs []FileObject) error {
	ordered := make([]FileObject, len(objs))
	copy(ordered, objs)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Key < ordered[j].Key })

	var total int64
	for _, o := range ordered {
		total += o.Size
	}
	manifest := FilesManifest{
		Prefix:     prefix,
		Count:      len(ordered),
		TotalBytes: total,
		Note:       filesManifestNote,
		Objects:    ordered,
	}
	b, err := marshalIndent(manifest)
	if err != nil {
		return err
	}
	return tree.Write(filesManifestFile, b)
}

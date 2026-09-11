package exporter

import "time"

// SchemaVersion is the account-export ZIP format version. Bump on breaking layout/shape changes so a
// consumer (or our own importer) can detect and adapt.
const SchemaVersion = 1

const manifestFile = "manifest.json"

// Manifest is the export's top-level index (manifest.json). It records the schema version, when the
// export ran, whose account it is, and per-section counts.
type Manifest struct {
	SchemaVersion int             `json:"schema_version"`
	ExportedAt    time.Time       `json:"exported_at"`
	Account       ManifestAccount `json:"account"`
	Counts        map[string]int  `json:"counts"`
}

// ManifestAccount identifies the exported account. Email/username are included because this is the
// owner's own copy of their data; no other users' data appears in the export.
type ManifestAccount struct {
	UserID   string `json:"user_id"`
	Email    string `json:"email,omitempty"`
	Username string `json:"username,omitempty"`
}

// WriteManifest projects the manifest to the archive root.
func WriteManifest(tree Tree, m Manifest) error {
	m.SchemaVersion = SchemaVersion
	b, err := marshalIndent(m)
	if err != nil {
		return err
	}
	return tree.Write(manifestFile, b)
}

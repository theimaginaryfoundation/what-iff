// Package buildinfo carries build provenance stamped at compile time.
//
// The variables below are overridden with -ldflags -X at build time, e.g.
//
//	go build -ldflags "\
//	  -X github.com/theimaginaryfoundation/what-iff/internal/buildinfo.Version=v1.2.3 \
//	  -X github.com/theimaginaryfoundation/what-iff/internal/buildinfo.Commit=$(git rev-parse HEAD) \
//	  -X github.com/theimaginaryfoundation/what-iff/internal/buildinfo.BuiltAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
//
// A plain `go build` keeps the defaults, so a dev binary reports
// "dev"/"unknown" rather than lying about its provenance.
package buildinfo

var (
	// Version is the human-facing release identifier (a tag, or "dev").
	Version = "dev"
	// Commit is the full git SHA of the source tree the binary was built from.
	Commit = "unknown"
	// BuiltAt is the UTC build timestamp in RFC 3339 format.
	BuiltAt = "unknown"
	// OverlayCommit is empty in the open-source build. A downstream build
	// that composes additional source on top of this tree stamps the
	// revision of that overlay here; empty means "no overlay".
	OverlayCommit = ""
)

// Info is a snapshot of the stamped build provenance.
type Info struct {
	Version       string
	Commit        string
	BuiltAt       string
	OverlayCommit string
}

// Get returns the provenance stamped into this binary.
func Get() Info {
	return Info{
		Version:       Version,
		Commit:        Commit,
		BuiltAt:       BuiltAt,
		OverlayCommit: OverlayCommit,
	}
}

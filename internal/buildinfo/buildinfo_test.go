package buildinfo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGet(t *testing.T) {
	origVersion, origCommit, origBuiltAt, origOverlayCommit := Version, Commit, BuiltAt, OverlayCommit
	t.Cleanup(func() {
		Version, Commit, BuiltAt, OverlayCommit = origVersion, origCommit, origBuiltAt, origOverlayCommit
	})

	tests := []struct {
		name string
		set  func()
		want Info
	}{
		{
			name: "unstamped defaults",
			set: func() {
				Version, Commit, BuiltAt, OverlayCommit = "dev", "unknown", "unknown", ""
			},
			want: Info{Version: "dev", Commit: "unknown", BuiltAt: "unknown", OverlayCommit: ""},
		},
		{
			name: "stamped at build time",
			set: func() {
				Version, Commit, BuiltAt, OverlayCommit = "v1.2.3", "abc123", "2026-01-01T00:00:00Z", ""
			},
			want: Info{Version: "v1.2.3", Commit: "abc123", BuiltAt: "2026-01-01T00:00:00Z", OverlayCommit: ""},
		},
		{
			name: "overlay build stamps OverlayCommit",
			set: func() {
				Version, Commit, BuiltAt, OverlayCommit = "v1.2.3", "abc123", "2026-01-01T00:00:00Z", "def456"
			},
			want: Info{Version: "v1.2.3", Commit: "abc123", BuiltAt: "2026-01-01T00:00:00Z", OverlayCommit: "def456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set()
			assert.Equal(t, tt.want, Get())
		})
	}
}

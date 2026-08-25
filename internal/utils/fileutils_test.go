package utils

import "testing"

// TestFileTypesHasNoScriptableImageTypes guards the imagegallery raw-byte
// path: it serves any image/* attachment inline, so a scriptable image MIME
// in this allowlist would be stored XSS. If you're adding SVG support, land
// the nosniff/Content-Disposition hardening on that path first.
func TestFileTypesHasNoScriptableImageTypes(t *testing.T) {
	t.Parallel()

	for ext, info := range fileTypes {
		switch info.ContentType {
		case "image/svg+xml", "image/svg":
			t.Errorf("%s maps to scriptable image type %s: the imagegallery raw-byte path serves image/* inline without Content-Disposition, so this would be stored XSS. Add nosniff/Content-Disposition hardening to gallery.go before allowing SVG.", ext, info.ContentType)
		}
	}
}

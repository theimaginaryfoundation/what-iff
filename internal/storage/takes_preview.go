package storage

import (
	"github.com/theimaginaryfoundation/what-iff/internal/drawio"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// TakesPreview reports whether a file with this name and media type is given a preview: a picture
// of it the server cannot draw, sent by the browser that drew it. A draw.io diagram (ADR 0x049 §8)
// and a web page (ADR 0x050) are.
func TakesPreview(name, mediaType string) bool {
	return drawio.IsDiagramFile(name, mediaType) || models.IsHTMLFile(name, mediaType)
}

package exporter

import "archive/zip"

// ZipTree renders the projection into a zip archive (the MVP delivery format). It implements Tree by
// creating one archive entry per Write. Entries are written in call order; the projection writers
// order their own contents deterministically.
type ZipTree struct {
	zw *zip.Writer
}

// NewZipTree wraps a zip.Writer as a Tree sink. The caller owns closing the underlying writer.
func NewZipTree(zw *zip.Writer) *ZipTree {
	return &ZipTree{zw: zw}
}

func (t *ZipTree) Write(path string, content []byte) error {
	w, err := t.zw.Create(path)
	if err != nil {
		return err
	}
	_, err = w.Write(content)
	return err
}

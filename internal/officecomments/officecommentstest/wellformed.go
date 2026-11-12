// Package officecommentstest has checks the format packages' tests share.
package officecommentstest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// WellFormed reports the first XML part of an Office package that a strict parser refuses: one
// Office would refuse too. The packages' own readers are lenient, so a test that only reads its
// output back with them does not see a broken part.
func WellFormed(pkg []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(pkg), int64(len(pkg)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") && !strings.HasSuffix(f.Name, ".vml") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return err
		}
		d := xml.NewDecoder(bytes.NewReader(data))
		d.Strict = true
		for {
			_, err := d.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
		}
	}
	return nil
}

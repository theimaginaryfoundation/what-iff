package officecomments

import (
	"archive/zip"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestBudgetRefusesAPartThatInflatesPastIt(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("big.xml")
	_, _ = w.Write(bytes.Repeat([]byte{'a'}, 4000))
	w, _ = zw.Create("small.xml")
	_, _ = w.Write([]byte("<a/>"))
	_ = zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	b := &Budget{left: 1000}
	if _, err := b.Read(zr.File[0]); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big part: %v", err)
	}
	// A header that understates the size is refused too (archive/zip checks the size it reads).
	zr.File[0].UncompressedSize64 = 10
	if _, err := b.Read(zr.File[0]); err == nil {
		t.Fatal("lying header read")
	}
	if got, err := b.Read(zr.File[1]); err != nil || string(got) != "<a/>" {
		t.Fatalf("small part: %q %v", got, err)
	}
	if b.left != 1000-4 {
		t.Fatalf("budget left %d", b.left)
	}
}

func TestXMLTextDropsWhatXMLCannotHold(t *testing.T) {
	if got := XMLText("see \v here\f\x00 \t\n\r ok￾"); got != "see  here \t\n\r ok" {
		t.Fatalf("%q", got)
	}
	if got := XMLText(strings.Repeat("é", 3)); got != "ééé" {
		t.Fatalf("%q", got)
	}
}

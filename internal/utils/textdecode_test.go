package utils

import "testing"

func TestDecodeTextToUTF8_UTF8(t *testing.T) {
	t.Parallel()

	got, err := DecodeTextToUTF8([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
}

func TestDecodeTextToUTF8_UTF8BOM(t *testing.T) {
	t.Parallel()

	got, err := DecodeTextToUTF8([]byte{0xEF, 0xBB, 0xBF, 'h', 'i'})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hi" {
		t.Fatalf("expected hi, got %q", got)
	}
}

func TestDecodeTextToUTF8_UTF16LEBOM(t *testing.T) {
	t.Parallel()

	// "hi" in UTF-16LE with BOM: FF FE 68 00 69 00
	got, err := DecodeTextToUTF8([]byte{0xFF, 0xFE, 0x68, 0x00, 0x69, 0x00})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hi" {
		t.Fatalf("expected hi, got %q", got)
	}
}

func TestDecodeTextToUTF8_UTF16BEBOM(t *testing.T) {
	t.Parallel()

	// "hi" in UTF-16BE with BOM: FE FF 00 68 00 69
	got, err := DecodeTextToUTF8([]byte{0xFE, 0xFF, 0x00, 0x68, 0x00, 0x69})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "hi" {
		t.Fatalf("expected hi, got %q", got)
	}
}

func TestDecodeTextToUTF8_Invalid(t *testing.T) {
	t.Parallel()

	_, err := DecodeTextToUTF8([]byte{0xFF})
	if err == nil {
		t.Fatalf("expected error")
	}
}

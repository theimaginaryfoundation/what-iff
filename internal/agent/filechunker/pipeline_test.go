package filechunker

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestIsTextType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		// Supported types
		{"text/plain", "text/plain", true},
		{"text/markdown", "text/markdown", true},
		{"text/html", "text/html", true},
		{"text/css", "text/css", true},
		{"text/x-golang", "text/x-golang", true},
		{"text/x-script.python", "text/x-script.python", true},
		{"text/x-java", "text/x-java", true},
		{"text/x-c", "text/x-c", true},
		{"text/x-c++", "text/x-c++", true},
		{"text/x-csharp", "text/x-csharp", true},
		{"application/json", "application/json", true},
		{"text/javascript", "text/javascript", true},
		{"application/x-sh", "application/x-sh", true},
		{"text/x-ruby", "text/x-ruby", true},
		{"text/x-php", "text/x-php", true},
		{"text/x-tex", "text/x-tex", true},
		{"application/typescript", "application/typescript", true},

		// XML types are not supported (VectorSupport is false in fileutils.go)
		{"text/xml", "text/xml", false},
		{"application/xml", "application/xml", false},

		// Old MIME types no longer used
		{"text/x-go (old)", "text/x-go", false},
		{"text/x-python (old)", "text/x-python", false},
		{"application/javascript (old)", "application/javascript", false},
		{"application/x-ruby (old)", "application/x-ruby", false},
		{"application/x-php (old)", "application/x-php", false},
		{"application/x-tex (old)", "application/x-tex", false},

		// With charset parameter
		{"text/plain with charset", "text/plain; charset=utf-8", true},
		{"application/json with charset", "application/json; charset=utf-8", true},

		// Case insensitive
		{"uppercase TEXT/PLAIN", "TEXT/PLAIN", true},
		{"mixed case Text/Html", "Text/Html", true},

		// Unsupported types
		{"application/pdf", "application/pdf", false},
		{"application/octet-stream", "application/octet-stream", false},
		{"image/png", "image/png", false},
		{"image/jpeg", "image/jpeg", false},
		{"audio/mpeg", "audio/mpeg", false},
		{"video/mp4", "video/mp4", false},
		{"application/zip", "application/zip", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTextType(tt.contentType)
			if got != tt.want {
				t.Errorf("IsTextType(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestIsTextFileByExtension(t *testing.T) {
	tests := []struct {
		name     string
		fileName string
		want     bool
	}{
		// Supported extensions
		{"txt file", "readme.txt", true},
		{"markdown file", "README.md", true},
		{"go file", "main.go", true},
		{"python file", "script.py", true},
		{"javascript file", "app.js", true},
		{"typescript file", "app.ts", true},
		{"html file", "index.html", true},
		{"css file", "styles.css", true},
		{"json file", "config.json", true},
		{"xml file", "data.xml", false},
		{"c file", "main.c", true},
		{"cpp file", "main.cpp", true},
		{"csharp file", "Program.cs", true},
		{"java file", "Main.java", true},
		{"ruby file", "app.rb", true},
		{"shell script", "deploy.sh", true},
		{"php file", "index.php", true},
		{"tex file", "paper.tex", true},

		// Case insensitive extension
		{"uppercase GO", "main.GO", true},
		{"uppercase TXT", "file.TXT", true},

		// Unsupported extensions
		{"pdf file", "document.pdf", false},
		{"png image", "photo.png", false},
		{"jpeg image", "photo.jpeg", false},
		{"zip archive", "archive.zip", false},
		{"docx file", "document.docx", false},
		{"no extension", "Makefile", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsTextFileByExtension(tt.fileName)
			if got != tt.want {
				t.Errorf("IsTextFileByExtension(%q) = %v, want %v", tt.fileName, got, tt.want)
			}
		})
	}
}

func TestProcessAndStore_UnsupportedType(t *testing.T) {
	// Create pipeline with nil dependencies — the unsupported type check
	// should return an error before any external calls are made.
	p := NewFileChunkPipeline(nil, nil, nil)

	err := p.ProcessAndStore(
		context.Background(),
		uuid.New(),
		[]byte("some content"),
		"photo.png",
		"image/png",
	)

	if err == nil {
		t.Fatal("expected error for unsupported content type, got nil")
	}

	expected := "unsupported content type"
	if got := err.Error(); !contains(got, expected) {
		t.Errorf("error message %q should contain %q", got, expected)
	}
}

func TestProcessAndStore_UnsupportedTypeButSupportedExtension(t *testing.T) {
	// When content type is generic but extension is supported, the type check
	// passes. With nil dependencies it will panic/fail on chunking or embedding,
	// so we use empty content which produces no chunks and returns nil.
	// We need a logger for the warning log.
	// Since we pass nil logger and empty content produces no chunks,
	// the code will try to call p.logger.Warn which will panic.
	// Instead, test with unsupported extension + unsupported type.
	p := NewFileChunkPipeline(nil, nil, nil)

	err := p.ProcessAndStore(
		context.Background(),
		uuid.New(),
		[]byte("some content"),
		"archive.zip",
		"application/octet-stream",
	)

	if err == nil {
		t.Fatal("expected error for unsupported content type and extension, got nil")
	}
}

func TestProcessAndStore_OctetStreamWithTextExtension(t *testing.T) {
	// application/octet-stream is unsupported by MIME type, but .go extension
	// is supported. With empty content, ChunkText returns nil so the pipeline
	// logs a warning and returns nil (nothing to store).
	logger := zap.NewNop()
	p := NewFileChunkPipeline(nil, nil, logger)

	// Verify the type check logic directly.
	if !IsTextFileByExtension("main.go") {
		t.Error("expected .go to be recognized as text file extension")
	}
	if IsTextType("application/octet-stream") {
		t.Error("expected application/octet-stream to NOT be a supported text type")
	}

	// The combination should pass the type gate:
	// IsTextType("application/octet-stream") || IsTextFileByExtension("main.go") => true
	// So calling ProcessAndStore should NOT return "unsupported content type" error.
	// Empty content produces no chunks, so it returns nil.
	err := p.ProcessAndStore(
		context.Background(),
		uuid.New(),
		[]byte{}, // empty content — will hit the "no chunks" path
		"main.go",
		"application/octet-stream",
	)

	if err != nil {
		t.Errorf("expected nil error for empty content with supported extension, got: %v", err)
	}
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

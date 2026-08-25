package utils

import (
	"fmt"
	"path"
	"strings"
)

type FileTypeInfo struct {
	ContentType   string
	Extension     string
	VectorSupport bool
}

var fileTypes = map[string]FileTypeInfo{
	".c": {
		ContentType:   "text/x-c",
		Extension:     ".c",
		VectorSupport: true,
	},
	".cpp": {
		ContentType:   "text/x-c++",
		Extension:     ".cpp",
		VectorSupport: true,
	},
	".cs": {
		ContentType:   "text/x-csharp",
		Extension:     ".cs",
		VectorSupport: true,
	},
	".css": {
		ContentType:   "text/css",
		Extension:     ".css",
		VectorSupport: true,
	},
	".csv": {
		ContentType:   "text/csv",
		Extension:     ".csv",
		VectorSupport: false,
	},
	".doc": {
		ContentType:   "application/msword",
		Extension:     ".doc",
		VectorSupport: true,
	},
	".docx": {
		ContentType:   "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		Extension:     ".docx",
		VectorSupport: true,
	},
	".gif": {
		ContentType:   "image/gif",
		Extension:     ".gif",
		VectorSupport: false,
	},
	".go": {
		ContentType:   "text/x-golang",
		Extension:     ".go",
		VectorSupport: true,
	},
	".html": {
		ContentType:   "text/html",
		Extension:     ".html",
		VectorSupport: true,
	},
	".java": {
		ContentType:   "text/x-java",
		Extension:     ".java",
		VectorSupport: true,
	},
	".jpeg": {
		ContentType:   "image/jpeg",
		Extension:     ".jpeg",
		VectorSupport: false,
	},
	".jpg": {
		ContentType:   "image/jpeg",
		Extension:     ".jpg",
		VectorSupport: false,
	},
	".js": {
		ContentType:   "text/javascript",
		Extension:     ".js",
		VectorSupport: true,
	},
	".json": {
		ContentType:   "application/json",
		Extension:     ".json",
		VectorSupport: true,
	},
	".md": {
		ContentType:   "text/markdown",
		Extension:     ".md",
		VectorSupport: true,
	},
	".pdf": {
		ContentType:   "application/pdf",
		Extension:     ".pdf",
		VectorSupport: true,
	},
	".php": {
		ContentType:   "text/x-php",
		Extension:     ".php",
		VectorSupport: true,
	},
	".png": {
		ContentType:   "image/png",
		Extension:     ".png",
		VectorSupport: false,
	},
	".pptx": {
		ContentType:   "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		Extension:     ".pptx",
		VectorSupport: false,
	},
	".py": {
		ContentType:   "text/x-script.python",
		Extension:     ".py",
		VectorSupport: true,
	},
	".rb": {
		ContentType:   "text/x-ruby",
		Extension:     ".rb",
		VectorSupport: true,
	},
	".sh": {
		ContentType:   "application/x-sh",
		Extension:     ".sh",
		VectorSupport: true,
	},
	".tar": {
		ContentType:   "application/x-tar",
		Extension:     ".tar",
		VectorSupport: false,
	},
	".tex": {
		ContentType:   "text/x-tex",
		Extension:     ".tex",
		VectorSupport: true,
	},
	".tgz": {
		ContentType:   "application/x-tar",
		Extension:     ".tgz",
		VectorSupport: false,
	},
	".ts": {
		ContentType:   "application/typescript",
		Extension:     ".ts",
		VectorSupport: true,
	},
	".txt": {
		ContentType:   "text/plain",
		Extension:     ".txt",
		VectorSupport: true,
	},
	".xlsx": {
		ContentType:   "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		Extension:     ".xlsx",
		VectorSupport: false,
	},
	".xml": {
		ContentType:   "application/xml",
		Extension:     ".xml",
		VectorSupport: false,
	},
	".zip": {
		ContentType:   "application/zip",
		Extension:     ".zip",
		VectorSupport: false,
	},
}

func GetFileType(fileName string) (FileTypeInfo, error) {
	fileExt := strings.ToLower(path.Ext(fileName))
	fileType, ok := fileTypes[fileExt]
	if !ok {
		return FileTypeInfo{}, fmt.Errorf("unsupported file type: %s", fileExt)
	}

	return fileType, nil
}

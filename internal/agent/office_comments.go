package agent

import (
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments/docx"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments/pptx"
	"github.com/theimaginaryfoundation/what-iff/internal/officecomments/xlsx"
)

// officeCommentFormats are the Office formats whose comments the agent reads and writes (ADR
// 0x046), by file extension.
func officeCommentFormats() map[string]officecomments.Format {
	return map[string]officecomments.Format{
		".docx": docx.New(),
		".pptx": pptx.New(),
		".xlsx": xlsx.New(),
	}
}

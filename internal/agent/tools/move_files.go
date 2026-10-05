package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"go.uber.org/zap"
)

const (
	moveFilesToolName = "move_files"
	moveFilesMaxIDs   = 100
)

// MoveFilesDescription is the agent-facing description.
const MoveFilesDescription = `File gallery images in a folder, or move them back to the top level of the gallery.

Use it to tidy images that pile up, for example recurring charts or reports: after you have found them with list (kind="files", file_type="image"), move them to a folder such as "charts/oura". To file images as you create them, pass folder to generate_image instead.

Notes:
- ids are the ids list shows for the images. Only images in the user's gallery move; other ids are skipped, and the result says how many moved.
- folder is a path, e.g. "charts" or "charts/oura" ('/' nests, case-insensitive). Folders are created by moving something into them. Use "" to put images back at the top level.
- Only the label changes. The images are not copied, renamed or deleted.`

// MoveFilesToolSpec is the shared function-tool shape for move_files.
var MoveFilesToolSpec = FunctionToolSpec{
	Name:        moveFilesToolName,
	Description: MoveFilesDescription,
	Properties: map[string]interface{}{
		"ids": map[string]interface{}{
			"type":        "array",
			"description": fmt.Sprintf("Ids of the images to move, from list (kind=\"files\"). At most %d per call.", moveFilesMaxIDs),
			"items":       map[string]interface{}{"type": "string"},
			"minItems":    1,
			"maxItems":    moveFilesMaxIDs,
		},
		"folder": map[string]interface{}{
			"type":        "string",
			"description": "Destination gallery folder, e.g. 'charts/oura'. Use an empty string for the top level.",
		},
	},
	Required: []string{"ids", "folder"},
}

// moveFilesStore is the narrow datastore surface the move tool needs. *datastore.Datastore satisfies it.
type moveFilesStore interface {
	MoveFileAttachmentsToFolder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error)
}

// MoveFilesTool implements the move_files agent tool.
type MoveFilesTool struct {
	store  moveFilesStore
	logger *zap.Logger
}

// NewMoveFilesTool constructs a MoveFilesTool backed by the datastore.
func NewMoveFilesTool(ds *datastore.Datastore, logger *zap.Logger) *MoveFilesTool {
	return &MoveFilesTool{store: ds, logger: logger}
}

type moveFilesArgs struct {
	IDs    []string `json:"ids"`
	Folder string   `json:"folder"`
}

type moveFilesResult struct {
	Success   bool   `json:"success"`
	Folder    string `json:"folder"`
	Requested int    `json:"requested,omitempty"`
	Moved     int    `json:"moved,omitempty"`
	Note      string `json:"note,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Move executes a move_files call. A bad argument is reported to the model as an error result, not
// returned as a Go error, like the other tools.
func (t *MoveFilesTool) Move(ctx context.Context, chat *models.Chat, args []byte) (string, error) {
	fail := func(msg string) (string, error) {
		return marshalToolResult(moveFilesResult{Error: msg}, moveFilesToolName)
	}

	// The gallery is the owner's account-wide image library, outside a sandbox. The tool is not
	// offered there; this refuses a call the model makes anyway.
	if chat.IsSandboxed() {
		return fail(sandboxedNote("Sorting gallery images"))
	}

	var a moveFilesArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return fail(fmt.Sprintf("invalid arguments: %v", err))
	}
	if len(a.IDs) == 0 {
		return fail("ids is required: pass the ids of the images to move")
	}
	if len(a.IDs) > moveFilesMaxIDs {
		return fail(fmt.Sprintf("too many ids: move at most %d images per call", moveFilesMaxIDs))
	}
	ids := make([]uuid.UUID, 0, len(a.IDs))
	for _, raw := range a.IDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return fail(fmt.Sprintf("%q is not an image id; use the ids list shows", raw))
		}
		ids = append(ids, id)
	}
	folder, err := models.NormalizeFolder(a.Folder)
	if err != nil {
		return fail(fmt.Sprintf("invalid folder: %v", err))
	}

	moved, err := t.store.MoveFileAttachmentsToFolder(ctx, chat.UserID, ids, folder)
	if err != nil {
		t.logger.Error("move_files failed", zap.String("user_id", chat.UserID.String()), zap.Error(err))
		return fail("could not move the images; try again")
	}
	res := moveFilesResult{Success: true, Folder: folder, Requested: len(ids), Moved: moved}
	if moved < len(ids) {
		res.Note = "Some ids were not gallery images (a document, or an id that is not yours), or named the same image twice, so they were skipped."
	}
	return marshalToolResult(res, moveFilesToolName)
}

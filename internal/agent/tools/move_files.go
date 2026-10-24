package tools

import (
	"context"
	"encoding/json"
	"errors"
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
const MoveFilesDescription = `File the user's files (images, documents, any upload) in a gallery folder, move them back to the top level of the gallery, or rename one.

Use it to tidy files that pile up, for example recurring charts or reports: after you have found them with list (kind="files"), move them to a folder such as "charts/oura". Rename files whose names say nothing (an id, "image.png") to something that says what they are. To file images as you create them, pass folder to generate_image instead.

Notes:
- ids are the ids list shows for the files. Only the user's own files move; other ids are skipped, and the result says how many moved.
- folder is a path, e.g. "charts" or "charts/oura" ('/' nests, case-insensitive). Folders are created by moving something into them. Use "" to put files back at the top level. Leave folder out to rename without moving.
- name renames a single file: pass exactly one id. A name without an extension keeps the file's current one ("self-portrait" stays a .png). Names cannot contain '/'; use folder for that.
- Pass folder, name, or both. Only labels change. The files are not copied or deleted, and a personality's documents stay attached to it.`

// MoveFilesToolSpec is the shared function-tool shape for move_files.
var MoveFilesToolSpec = FunctionToolSpec{
	Name:        moveFilesToolName,
	Description: MoveFilesDescription,
	Properties: map[string]interface{}{
		"ids": map[string]interface{}{
			"type":        "array",
			"description": fmt.Sprintf("Ids of the files to move, from list (kind=\"files\"). At most %d per call; exactly one when renaming.", moveFilesMaxIDs),
			"items":       map[string]interface{}{"type": "string"},
			"minItems":    1,
			"maxItems":    moveFilesMaxIDs,
		},
		"folder": map[string]interface{}{
			"type":        "string",
			"description": "Destination gallery folder, e.g. 'charts/oura'. Use an empty string for the top level; leave it out to keep the files where they are.",
		},
		"name": map[string]interface{}{
			"type":        "string",
			"description": fmt.Sprintf("New display name for the one file in ids, e.g. 'vix-self-portrait-2025-03.png'. At most %d characters.", models.MaxFileNameRunes),
		},
	},
	Required: []string{"ids"},
}

// moveFilesStore is the narrow datastore surface the move tool needs. *datastore.Datastore satisfies it.
type moveFilesStore interface {
	MoveFileAttachmentsToFolder(ctx context.Context, userID uuid.UUID, ids []uuid.UUID, folder string) (int, error)
	RenameGalleryFile(ctx context.Context, userID, id uuid.UUID, name string) (string, error)
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
	Folder *string  `json:"folder"` // nil: leave the files where they are
	Name   *string  `json:"name"`   // nil: keep the name
}

type moveFilesResult struct {
	Success   bool    `json:"success"`
	Folder    *string `json:"folder,omitempty"`
	Requested int     `json:"requested,omitempty"`
	Moved     int     `json:"moved,omitempty"`
	Name      string  `json:"name,omitempty"`
	Note      string  `json:"note,omitempty"`
	Error     string  `json:"error,omitempty"`
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
		return fail("ids is required: pass the ids of the files to move")
	}
	if len(a.IDs) > moveFilesMaxIDs {
		return fail(fmt.Sprintf("too many ids: move at most %d files per call", moveFilesMaxIDs))
	}
	if a.Folder == nil && a.Name == nil {
		return fail("pass folder to move the files, name to rename one, or both")
	}
	if a.Name != nil && len(a.IDs) != 1 {
		return fail("name renames one file: pass exactly one id with it")
	}
	ids := make([]uuid.UUID, 0, len(a.IDs))
	for _, raw := range a.IDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			return fail(fmt.Sprintf("%q is not a file id; use the ids list shows", raw))
		}
		ids = append(ids, id)
	}
	var folder string
	if a.Folder != nil {
		var err error
		if folder, err = models.NormalizeFolder(*a.Folder); err != nil {
			return fail(fmt.Sprintf("invalid folder: %v", err))
		}
	}
	// The rename goes first, so a name the store refuses (it alone knows the extension the name
	// inherits) stops the call before anything moves.
	res := moveFilesResult{Success: true}
	if a.Name != nil {
		name, err := t.store.RenameGalleryFile(ctx, chat.UserID, ids[0], *a.Name)
		switch {
		case errors.Is(err, datastore.ErrFileAttachmentNotFound):
			return fail("that id is not one of your files; use the ids list shows")
		case errors.Is(err, models.ErrInvalidFileName):
			return fail(err.Error())
		case err != nil:
			t.logger.Error("move_files rename failed", zap.String("user_id", chat.UserID.String()), zap.Error(err))
			return fail("could not rename the file; try again")
		}
		res.Name = name
	}
	if a.Folder != nil {
		moved, err := t.store.MoveFileAttachmentsToFolder(ctx, chat.UserID, ids, folder)
		if err != nil {
			t.logger.Error("move_files failed", zap.String("user_id", chat.UserID.String()), zap.Error(err))
			msg := "could not move the files; try again"
			if res.Name != "" {
				msg = fmt.Sprintf("renamed the file to %q, but could not move it; try the move again", res.Name)
			}
			return fail(msg)
		}
		res.Folder, res.Requested, res.Moved = &folder, len(ids), moved
		if moved < len(ids) {
			res.Note = "Some ids were not your files, or named the same file twice, so they were skipped."
		}
	}
	return marshalToolResult(res, moveFilesToolName)
}

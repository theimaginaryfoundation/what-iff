package chat

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/filechange"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func (h *Handler) CreateFileAttachment(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserIDFromContext(r.Context())
	if !ok {
		handlerutils.RespondWithError(w, h.logger, http.StatusUnauthorized, handlerutils.CodeNotSet, "Unauthorized", nil)
		return
	}

	vars := mux.Vars(r)
	chatIDStr := vars["chatId"]

	chatID, err := uuid.Parse(chatIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid chat ID", err)
		return
	}

	fileAttachment, tempFilePath, err := handlerutils.UploadFileAttachment(w, r, h.logger, userID)
	if err != nil {
		return
	}
	// An optional gallery folder (ADR 0x043 §3: Save As from the chat-hosted workspace files the
	// new file where the user chose and attaches it to the conversation in one upload).
	if rawFolder := strings.TrimSpace(r.FormValue("folder")); rawFolder != "" {
		folder, folderErr := models.NormalizeFolder(rawFolder)
		if folderErr == nil && models.IsReservedFolder(folder) {
			folderErr = models.ErrReservedFolder
		}
		if folderErr != nil {
			handlerutils.AbandonFileAttachmentUpload(r.Context(), fileAttachment, tempFilePath)
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, folderErr.Error(), folderErr)
			return
		}
		fileAttachment.Folder = folder
	}

	createdAttachment, err := handlerutils.StoreChatAttachment(r.Context(), h.logger, handlerutils.AttachmentStorage{
		Records:  h.ds,
		Files:    h.agent.FileStore(),
		Pipeline: h.agent.ChunkPipeline(),
	}, userID, chatID, fileAttachment, tempFilePath)
	if err != nil {
		handlerutils.RespondWithUploadError(w, h.logger, err)
		return
	}

	filechange.Notify(r.Context(), filechange.Event{Kind: filechange.KindCreate, UserID: userID, File: createdAttachment, ChatID: &chatID})

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, createdAttachment)
}

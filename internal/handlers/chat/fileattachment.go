package chat

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/theimaginaryfoundation/what-iff/internal/filechange"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/middleware"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// maxUploadDescriptionRunes caps an upload's description (openapi: maxLength 500).
const maxUploadDescriptionRunes = 500

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

	// An optional description, which the agent sees beside the file's name (ADR 0x045: the markup
	// tool says what a mask or a markup copy is).
	if description := strings.TrimSpace(r.FormValue("description")); description != "" {
		if utf8.RuneCountInString(description) > maxUploadDescriptionRunes {
			handlerutils.AbandonFileAttachmentUpload(r.Context(), fileAttachment, tempFilePath)
			handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "description is too long", nil)
			return
		}
		fileAttachment.Description = &description
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

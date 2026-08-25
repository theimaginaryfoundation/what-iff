package role

import (
	"encoding/json"
	"net/http"

	"github.com/theimaginaryfoundation/what-iff/ent"
	"github.com/theimaginaryfoundation/what-iff/internal/datastore"
	"github.com/theimaginaryfoundation/what-iff/internal/handlers/handlerutils"
	"github.com/theimaginaryfoundation/what-iff/internal/models"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// CreateRole creates a new role
func (h *Handler) CreateRole(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req models.CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate required fields
	if req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Role name is required", nil)
		return
	}

	// Create role
	role, err := h.ds.CreateRole(r.Context(), req)
	if err == datastore.ErrRoleNameExists {
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Role name already exists", err)
		return
	} else if err == datastore.ErrInvalidRoleName {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid role name", err)
		return
	} else if err != nil {
		h.logger.Error("failed to create role", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to create role", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusCreated, role)
}

// ListRoles returns a paginated list of roles
func (h *Handler) ListRoles(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()

	// Parse pagination parameters
	page := handlerutils.ParseIntParam(queryParams.Get("page"), 1)
	pageSize := handlerutils.ParseIntParam(queryParams.Get("limit"), 10)

	// Parse filter parameters
	searchQuery := queryParams.Get("search")

	filters := models.RoleFilters{}
	if searchQuery != "" {
		filters.Search = &searchQuery
	}

	// Get paginated roles
	rolePage, err := h.ds.ListRoles(r.Context(), page, pageSize, filters)
	if err != nil {
		h.logger.Error("failed to list roles", zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to list roles", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, rolePage)
}

// GetRole retrieves a specific role by ID
func (h *Handler) GetRole(w http.ResponseWriter, r *http.Request) {
	// Get role ID from URL
	vars := mux.Vars(r)
	roleIDStr, ok := vars["id"]
	if !ok || roleIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Role ID is required", nil)
		return
	}

	roleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid role ID", err)
		return
	}

	// Get role
	role, err := h.ds.GetRole(r.Context(), roleID)
	if ent.IsNotFound(err) || err == datastore.ErrRoleNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Role not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to get role",
			zap.String("role_id", roleID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to get role", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, role)
}

// UpdateRole updates an existing role
func (h *Handler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	// Get role ID from URL
	vars := mux.Vars(r)
	roleIDStr, ok := vars["id"]
	if !ok || roleIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Role ID is required", nil)
		return
	}

	roleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid role ID", err)
		return
	}

	// Parse request body
	var req models.UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid request body", err)
		return
	}

	// Validate that at least one field is being updated
	if req.Name == nil && req.Description == nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "At least one field must be provided for update", nil)
		return
	}

	// Validate name if provided
	if req.Name != nil && *req.Name == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Role name cannot be empty", nil)
		return
	}

	// Update role
	role, err := h.ds.UpdateRole(r.Context(), roleID, req)
	if ent.IsNotFound(err) || err == datastore.ErrRoleNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Role not found", err)
		return
	} else if err == datastore.ErrRoleNameExists {
		handlerutils.RespondWithError(w, h.logger, http.StatusConflict, handlerutils.CodeNotSet, "Role name already exists", err)
		return
	} else if err != nil {
		h.logger.Error("failed to update role",
			zap.String("role_id", roleID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to update role", err)
		return
	}

	handlerutils.RespondWithJSON(w, h.logger, http.StatusOK, role)
}

// DeleteRole deletes a role
func (h *Handler) DeleteRole(w http.ResponseWriter, r *http.Request) {
	// Get role ID from URL
	vars := mux.Vars(r)
	roleIDStr, ok := vars["id"]
	if !ok || roleIDStr == "" {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Role ID is required", nil)
		return
	}

	roleID, err := uuid.Parse(roleIDStr)
	if err != nil {
		handlerutils.RespondWithError(w, h.logger, http.StatusBadRequest, handlerutils.CodeNotSet, "Invalid role ID", err)
		return
	}

	// Delete role
	err = h.ds.DeleteRole(r.Context(), roleID)
	if ent.IsNotFound(err) || err == datastore.ErrRoleNotFound {
		handlerutils.RespondWithError(w, h.logger, http.StatusNotFound, handlerutils.CodeNotSet, "Role not found", err)
		return
	} else if err != nil {
		h.logger.Error("failed to delete role",
			zap.String("role_id", roleID.String()),
			zap.Error(err))
		handlerutils.RespondWithError(w, h.logger, http.StatusInternalServerError, handlerutils.CodeNotSet, "Failed to delete role", err)
		return
	}

	handlerutils.RespondWithNoContent(w)
}

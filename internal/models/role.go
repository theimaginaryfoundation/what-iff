package models

import (
	"time"

	"github.com/google/uuid"
)

// Role represents a role entity
type Role struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateRoleRequest represents the data needed to create a new role
type CreateRoleRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

// UpdateRoleRequest represents the data needed to update a role
type UpdateRoleRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// RoleFilters represents filter criteria for listing roles
type RoleFilters struct {
	Search *string `json:"search,omitempty"`
}

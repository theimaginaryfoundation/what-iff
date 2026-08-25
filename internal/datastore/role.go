package datastore

import (
	"context"
	"errors"

	"github.com/theimaginaryfoundation/what-iff/ent"
	entrole "github.com/theimaginaryfoundation/what-iff/ent/role"
	"github.com/theimaginaryfoundation/what-iff/internal/i18n"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
	"github.com/theimaginaryfoundation/what-iff/internal/utils"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	ErrRoleNotFound    = errors.New("role not found")
	ErrRoleNameExists  = errors.New("role name already exists")
	ErrInvalidRoleName = errors.New("invalid role name")
)

// Convert from Ent role to model
func toRoleModel(e *ent.Role) *models.Role {
	if e == nil {
		return nil
	}

	roleModel := models.Role{
		ID:        e.ID,
		Name:      e.Name,
		CreatedAt: e.CreatedAt,
		UpdatedAt: e.UpdatedAt,
	}

	// Convert empty string to nil for optional description
	if e.Description != "" {
		roleModel.Description = &e.Description
	}

	return &roleModel
}

// CreateRole creates a new role in the datastore
func (d *Datastore) CreateRole(ctx context.Context, req models.CreateRoleRequest) (*models.Role, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Validate name
	if req.Name == "" {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrInvalidRoleName
	}

	// Check if role name already exists
	exists, err := tx.Role.Query().
		Where(entrole.Name(req.Name)).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T("role.name_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	if exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, ErrRoleNameExists
	}

	// Create role
	create := tx.Role.Create().
		SetName(req.Name)

	if req.Description != nil && *req.Description != "" {
		create.SetDescription(*req.Description)
	}

	entRole, err := create.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("create.failed", "Entity", "role"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toRoleModel(entRole), nil
}

// GetRole retrieves a role by ID
func (d *Datastore) GetRole(ctx context.Context, roleID uuid.UUID) (*models.Role, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	entRole, err := tx.Role.Query().
		Where(entrole.ID(roleID)).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRoleNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "role"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toRoleModel(entRole), nil
}

// ListRoles returns a paginated list of roles
func (d *Datastore) ListRoles(ctx context.Context, pageNum, pageSize int, filters models.RoleFilters) (*models.PaginatedResponse, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Build query
	query := tx.Role.Query()

	// Apply search filter if provided
	if filters.Search != nil && *filters.Search != "" {
		query = query.Where(
			entrole.Or(
				entrole.NameContainsFold(*filters.Search),
				entrole.DescriptionContainsFold(*filters.Search),
			),
		)
	}

	// Get total count
	totalCount, err := query.Count(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("count.failed", "Entity", "roles"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Normalize pagination parameters
	pagination := utils.NormalizePagination(pageNum, pageSize)
	query = query.
		Offset(pagination.Offset).
		Limit(pagination.PageSize).
		Order(ent.Desc(entrole.FieldCreatedAt))

	// Execute query
	entRoles, err := query.All(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("list.failed", "Entity", "roles"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Convert to model types
	roleModels := make([]any, len(entRoles))
	for i, entRole := range entRoles {
		roleModels[i] = toRoleModel(entRole)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return utils.BuildPaginatedResponse(roleModels, totalCount, pagination), nil
}

// UpdateRole updates an existing role
func (d *Datastore) UpdateRole(ctx context.Context, roleID uuid.UUID, req models.UpdateRoleRequest) (*models.Role, error) {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return nil, err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if role exists
	entRole, err := tx.Role.Query().
		Where(entrole.ID(roleID)).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRoleNotFound
		}
		d.logger.Error(i18n.T1("query.failed", "Entity", "role"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Check if name is being updated and if it conflicts
	if req.Name != nil && *req.Name != "" && *req.Name != entRole.Name {
		exists, err := tx.Role.Query().
			Where(entrole.Name(*req.Name)).
			Exist(ctx)

		if err != nil {
			d.logger.Error(i18n.T("role.name_check_failed"), zap.Error(err))
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, err
		}

		if exists {
			if rerr := tx.Rollback(); rerr != nil {
				d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
			}
			return nil, ErrRoleNameExists
		}
	}

	// Build update query
	update := tx.Role.Update().
		Where(entrole.ID(roleID))

	if req.Name != nil && *req.Name != "" {
		update.SetName(*req.Name)
	}

	if req.Description != nil {
		if *req.Description == "" {
			update.ClearDescription()
		} else {
			update.SetDescription(*req.Description)
		}
	}

	// Execute update
	_, err = update.Save(ctx)
	if err != nil {
		d.logger.Error(i18n.T1("update.failed", "Entity", "role"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Reload the role to get updated timestamps
	entRole, err = tx.Role.Query().
		Where(entrole.ID(roleID)).
		Only(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("reload.failed", "Entity", "role"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return nil, err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return nil, err
	}

	return toRoleModel(entRole), nil
}

// DeleteRole deletes a role by ID
func (d *Datastore) DeleteRole(ctx context.Context, roleID uuid.UUID) error {
	// Start transaction
	tx, err := d.dbClient.Tx(ctx)
	if err != nil {
		d.logger.Error(i18n.T("tx.start_failed"), zap.Error(err))
		return err
	}

	// Rollback in case of error
	defer func() {
		if v := recover(); v != nil {
			tx.Rollback()
			panic(v)
		}
	}()

	// Check if role exists
	exists, err := tx.Role.Query().
		Where(entrole.ID(roleID)).
		Exist(ctx)

	if err != nil {
		d.logger.Error(i18n.T("role.existence_check_failed"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	if !exists {
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return ErrRoleNotFound
	}

	// Delete role
	err = tx.Role.DeleteOneID(roleID).Exec(ctx)

	if err != nil {
		d.logger.Error(i18n.T1("delete.failed", "Entity", "role"), zap.Error(err))
		if rerr := tx.Rollback(); rerr != nil {
			d.logger.Error(i18n.T("tx.rollback_failed"), zap.Error(rerr))
		}
		return err
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		d.logger.Error(i18n.T("tx.commit_failed"), zap.Error(err))
		return err
	}

	return nil
}

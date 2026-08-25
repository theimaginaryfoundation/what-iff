package utils

import (
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

// PaginationParams holds normalized pagination parameters
type PaginationParams struct {
	Page     int
	PageSize int
	Offset   int
}

// NormalizePagination normalizes pagination parameters and returns PaginationParams
// Defaults: pageNum = 1, pageSize = 10
func NormalizePagination(pageNum, pageSize int) PaginationParams {
	if pageNum < 1 {
		pageNum = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}

	return PaginationParams{
		Page:     pageNum,
		PageSize: pageSize,
		Offset:   (pageNum - 1) * pageSize,
	}
}

// CalculateTotalPages calculates the total number of pages
func CalculateTotalPages(totalCount, pageSize int) int {
	if totalCount == 0 {
		return 1
	}
	totalPages := (totalCount + pageSize - 1) / pageSize
	if totalPages < 1 {
		return 1
	}
	return totalPages
}

// BuildPaginatedResponse creates a PaginatedResponse from results and pagination info
func BuildPaginatedResponse(results []any, totalCount int, params PaginationParams) *models.PaginatedResponse {
	totalPages := CalculateTotalPages(totalCount, params.PageSize)

	return &models.PaginatedResponse{
		Results:    results,
		TotalCount: totalCount,
		Page:       params.Page,
		TotalPages: totalPages,
	}
}

package models

type PaginatedResponse struct {
	Results    []any `json:"results"`
	TotalCount int   `json:"total_count"`
	TotalPages int   `json:"totalPages,omitempty"`
	Page       int   `json:"page"`
}

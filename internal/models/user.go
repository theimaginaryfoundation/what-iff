package models

import (
	"time"

	"github.com/google/uuid"
)

// UserRegisterRequest represents the data needed to register a new user
type UserRegisterRequest struct {
	Username      string `json:"username"`
	Email         string `json:"email"`
	Password      string `json:"password"`
	FirstName     string `json:"first_name,omitempty"`
	LastName      string `json:"last_name,omitempty"`
	TermsAccepted bool   `json:"terms_accepted"`
}

// UserLoginRequest represents the data needed to log in
type UserLoginRequest struct {
	Username string `json:"username"` // Could be username or email
	Password string `json:"password"`
}

// UserResponse represents the user data sent back to the client
type UserResponse struct {
	ID                       uuid.UUID `json:"id"`
	Username                 string    `json:"username"`
	Email                    string    `json:"email"`
	FirstName                string    `json:"first_name,omitempty"`
	LastName                 string    `json:"last_name,omitempty"`
	Timezone                 string    `json:"timezone,omitempty"`
	Role                     string    `json:"role"`
	Status                   string    `json:"status"`
	EnableExperimentalModels bool      `json:"enable_experimental_models"`
	Theme                    string    `json:"theme,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// TokenPair represents a pair of access and refresh tokens
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// LoginResponse represents the response after successful login
type LoginResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	User         UserResponse `json:"user"`
}

// UpdatePasswordRequest represents the data needed to update a password
type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// UpdateUserRequest represents the data needed to update user profile
type UpdateUserRequest struct {
	Email     string `json:"email,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
}

// RefreshTokenRequest represents a request to refresh an access token
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshTokenResponse represents the response to a refresh token request
type RefreshTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// AdminUpdateUserRequest represents the data needed to update a user (admin only)
type AdminUpdateUserRequest struct {
	Username  string `json:"username,omitempty"`
	Email     string `json:"email,omitempty"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	Timezone  string `json:"timezone,omitempty"`
	Role      string `json:"role,omitempty"`
	Status    string `json:"status,omitempty"`
}

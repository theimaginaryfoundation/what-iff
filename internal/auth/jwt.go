package auth

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	AccessTokenExpiration  = 2 * time.Hour
	RefreshTokenExpiration = 14 * 24 * time.Hour
)

type TokenClaims struct {
	UserID uuid.UUID `json:"user_id"`
	Role   string    `json:"role"`
	jwt.RegisteredClaims
}

type RefreshTokenClaims struct {
	UserID  uuid.UUID `json:"user_id"`
	Role    string    `json:"role"`
	TokenID string    `json:"token_id"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// GenerateTokenPair creates both an access token and a refresh token for a user
func GenerateTokenPair(userID uuid.UUID) (*TokenPair, string, error) {
	// Generate a new refresh token ID
	refreshTokenID := uuid.New().String()

	// Generate access token
	accessToken, err := generateAccessToken(userID)
	if err != nil {
		return nil, "", fmt.Errorf("error generating access token: %w", err)
	}

	// Generate refresh token
	refreshToken, err := generateRefreshToken(userID, refreshTokenID)
	if err != nil {
		return nil, "", fmt.Errorf("error generating refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, refreshTokenID, nil
}

// generateAccessToken creates a new short-lived JWT access token
func generateAccessToken(userID uuid.UUID) (string, error) {
	claims := TokenClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   userID.String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret := []byte(os.Getenv("JWT_SECRET"))
	if len(secret) == 0 {
		return "", errors.New("JWT_SECRET environment variable not set")
	}

	return token.SignedString(secret)
}

// generateRefreshToken creates a new long-lived refresh token with a token ID
func generateRefreshToken(userID uuid.UUID, tokenID string) (string, error) {
	claims := RefreshTokenClaims{
		UserID:  userID,
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(RefreshTokenExpiration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   userID.String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	secret := []byte(os.Getenv("JWT_REFRESH_SECRET"))
	if len(secret) == 0 {
		return "", errors.New("JWT_REFRESH_SECRET environment variable not set")
	}

	return token.SignedString(secret)
}

// ValidateAccessToken validates the JWT access token and returns the claims.
// The signing method is pinned to HS256 exactly (not the HMAC family) so a
// token signed with any other algorithm is rejected outright.
func ValidateAccessToken(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &TokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("JWT_SECRET")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*TokenClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}

// ValidateRefreshToken validates the refresh token and returns the claims.
// Pinned to HS256 exactly, same as ValidateAccessToken.
func ValidateRefreshToken(tokenString string) (*RefreshTokenClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &RefreshTokenClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(os.Getenv("JWT_REFRESH_SECRET")), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*RefreshTokenClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid refresh token")
}

// RefreshAccessToken generates a new access token using a valid refresh token
func RefreshAccessToken(refreshToken string) (string, error) {
	claims, err := ValidateRefreshToken(refreshToken)
	if err != nil {
		return "", fmt.Errorf("invalid refresh token: %w", err)
	}

	return generateAccessToken(claims.UserID)
}

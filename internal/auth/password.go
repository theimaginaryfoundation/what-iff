package auth

import (
	"os"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// HashPassword hashes a password using bcrypt
func HashPassword(password string) (string, error) {
	// Get bcrypt cost from environment or use default
	costStr := os.Getenv("BCRYPT_COST")
	cost := bcrypt.DefaultCost

	if costStr != "" {
		parsedCost, err := strconv.Atoi(costStr)
		if err == nil && parsedCost > 0 {
			cost = parsedCost
		}
	}

	bytes, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPassword compares a password with a hash
func CheckPassword(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// ExternalAuthPasswordPlaceholder marks accounts whose authentication is handled
// by an upstream identity provider, so they have no local password in our
// database. It is a single sentinel value shared by every package that reads or
// writes it, and cannot collide with a real bcrypt hash.
//
// NOTE: the literal value is retained for backward compatibility — existing
// provider-provisioned rows already store this exact string, so changing it
// would require a data migration.
const ExternalAuthPasswordPlaceholder = "!COGNITO_AUTH_NO_PASSWORD_HASH!"

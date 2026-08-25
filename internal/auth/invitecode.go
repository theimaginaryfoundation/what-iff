package auth

import (
	"github.com/google/uuid"
	"strings"
)

func CreateInviteCode() (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}

	return strings.Join(strings.Split(u.String(), "-")[:2], ""), nil
}

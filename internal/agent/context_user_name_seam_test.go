package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestContextUserNameDefaultsToUsername(t *testing.T) {
	a := &Agent{}
	u := &models.UserResponse{Username: "ada_l", FirstName: "Ada"}
	require.Nil(t, contextUserNameForUser, "inert in the open-source build")
	require.Equal(t, "ada_l", a.contextUserName(context.Background(), u), "no seam: the username, even with a first name set")
	require.Equal(t, "", a.contextUserName(context.Background(), nil))
}

func TestContextUserNameSeam(t *testing.T) {
	prev := contextUserNameForUser
	t.Cleanup(func() { contextUserNameForUser = prev })
	a := &Agent{}

	contextUserNameForUser = func(_ context.Context, _ *Agent, u *models.UserResponse) string { return u.FirstName }
	require.Equal(t, "Ada", a.contextUserName(context.Background(), &models.UserResponse{Username: "t_0123", FirstName: "Ada"}))
	require.Equal(t, "t_0123", a.contextUserName(context.Background(), &models.UserResponse{Username: "t_0123"}),
		"an empty answer keeps the username")

	contextUserNameForUser = func(context.Context, *Agent, *models.UserResponse) string { return "   " }
	require.Equal(t, "t_0123", a.contextUserName(context.Background(), &models.UserResponse{Username: "t_0123"}),
		"a blank answer keeps the username")
	require.Equal(t, []string{"The user's name is Ada"},
		initialTurnContextLines(userNameFor(a, "Ada"), false, false))
}

// userNameFor runs a name through the helper the way getMemories does.
func userNameFor(a *Agent, first string) string {
	prev := contextUserNameForUser
	defer func() { contextUserNameForUser = prev }()
	contextUserNameForUser = func(_ context.Context, _ *Agent, u *models.UserResponse) string { return u.FirstName }
	return a.contextUserName(context.Background(), &models.UserResponse{Username: "t_0123", FirstName: first})
}

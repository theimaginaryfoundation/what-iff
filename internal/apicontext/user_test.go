package apicontext

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestWithUserIDAndUserIDFrom(t *testing.T) {
	tests := []struct {
		name   string
		seed   func(ctx context.Context) context.Context
		wantID uuid.UUID
		wantOK bool
	}{
		{
			name: "context carries the user id it was given",
			seed: func(ctx context.Context) context.Context {
				return WithUserID(ctx, uuid.MustParse("11111111-1111-1111-1111-111111111111"))
			},
			wantID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			wantOK: true,
		},
		{
			name: "bare context has no user id",
			seed: func(ctx context.Context) context.Context {
				return ctx
			},
			wantID: uuid.Nil,
			wantOK: false,
		},
		{
			name: "nil uuid is still a present value",
			seed: func(ctx context.Context) context.Context {
				return WithUserID(ctx, uuid.Nil)
			},
			wantID: uuid.Nil,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.seed(context.Background())
			id, ok := UserIDFrom(ctx)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantID, id)
		})
	}
}

func TestUserIDFromWrongValueType(t *testing.T) {
	// A context.Value under an unrelated key type must not be mistaken for
	// the user id, even if by some other package's bug it stored a string
	// under an identically-named-but-distinct key type.
	ctx := context.WithValue(context.Background(), struct{ marker string }{"userIDKey{}"}, "not-a-uuid")
	id, ok := UserIDFrom(ctx)
	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, id)
}

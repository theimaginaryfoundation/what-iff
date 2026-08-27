package featuregate

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeGate struct {
	entitled bool
	calledID uuid.UUID
}

func (g *fakeGate) IsEntitled(_ context.Context, userID uuid.UUID) bool {
	g.calledID = userID
	return g.entitled
}

func TestIsEntitled(t *testing.T) {
	origActive := Active
	t.Cleanup(func() { Active = origActive })

	userID := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	tests := []struct {
		name string
		gate Gate
		want bool
	}{
		{
			name: "no gate linked: every feature is available",
			gate: nil,
			want: true,
		},
		{
			name: "linked gate denies",
			gate: &fakeGate{entitled: false},
			want: false,
		},
		{
			name: "linked gate allows",
			gate: &fakeGate{entitled: true},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Active = tt.gate
			got := IsEntitled(context.Background(), userID)
			assert.Equal(t, tt.want, got)
			if fg, ok := tt.gate.(*fakeGate); ok {
				assert.Equal(t, userID, fg.calledID, "gate should be invoked with the same userID")
			}
		})
	}
}

package featuregate

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeExperimentalStore struct {
	has bool
	err error
}

func (s fakeExperimentalStore) UserHasExperimentalFeature(context.Context, uuid.UUID, string) (bool, error) {
	return s.has, s.err
}

func TestExperimentalEnabled(t *testing.T) {
	orig := ActiveExperimental
	t.Cleanup(func() { ActiveExperimental = orig })
	ctx := context.Background()
	userID := uuid.New()

	tests := []struct {
		name string
		gate ExperimentalGate
		want bool
	}{
		{"no gate linked: the operator switch alone decides", nil, true},
		{"user flag on", UserFlagExperimentalGate{DS: fakeExperimentalStore{has: true}}, true},
		{"user flag off", UserFlagExperimentalGate{DS: fakeExperimentalStore{has: false}}, false},
		{"lookup fails closed", UserFlagExperimentalGate{DS: fakeExperimentalStore{has: true, err: errors.New("db down")}}, false},
		{"gate without a store fails closed", UserFlagExperimentalGate{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ActiveExperimental = tt.gate
			assert.Equal(t, tt.want, ExperimentalEnabled(ctx, userID, "buzz_relay"))
		})
	}
}

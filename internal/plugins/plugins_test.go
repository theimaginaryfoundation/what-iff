package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// withCleanRegistrars runs fn with the package-level registrars slice reset
// to empty, restoring whatever was there afterward. Register/Apply operate
// on shared package state, so tests must not leak registrations into each
// other or into the rest of the suite.
func withCleanRegistrars(t *testing.T, fn func()) {
	t.Helper()
	orig := registrars
	registrars = nil
	t.Cleanup(func() { registrars = orig })
	fn()
}

func TestApplyWithNoRegistrars(t *testing.T) {
	withCleanRegistrars(t, func() {
		// Must not panic and must not call anything.
		assert.NotPanics(t, func() { Apply(Deps{}) })
	})
}

func TestRegisterAndApply(t *testing.T) {
	withCleanRegistrars(t, func() {
		var calls []Deps

		Register(func(d Deps) { calls = append(calls, d) })
		Register(func(d Deps) { calls = append(calls, d) })

		want := Deps{}
		Apply(want)

		assert.Len(t, calls, 2, "every registered registrar should run exactly once per Apply")
		for _, got := range calls {
			assert.Equal(t, want, got, "each registrar should receive the same Deps passed to Apply")
		}
	})
}

func TestApplyRunsRegistrarsInRegistrationOrder(t *testing.T) {
	withCleanRegistrars(t, func() {
		var order []int

		Register(func(Deps) { order = append(order, 1) })
		Register(func(Deps) { order = append(order, 2) })
		Register(func(Deps) { order = append(order, 3) })

		Apply(Deps{})

		assert.Equal(t, []int{1, 2, 3}, order)
	})
}

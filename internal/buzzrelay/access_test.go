package buzzrelay

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnabledIsOptIn(t *testing.T) {
	for v, want := range map[string]bool{"": false, "false": false, "0": false, "nope": false, "true": true, "1": true, " ON ": true, "yes": true} {
		t.Setenv("BUZZ_RELAY_ENABLED", v)
		assert.Equal(t, want, Enabled(), "BUZZ_RELAY_ENABLED=%q", v)
	}
}

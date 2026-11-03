package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func TestParseUpdatePlanArgs(t *testing.T) {
	plan, err := ParseUpdatePlanArgs([]byte(`{"items":[{"content":"  read docs ","status":"IN_PROGRESS"},{"content":"write","status":"pending"}]}`))
	require.NoError(t, err)
	assert.Equal(t, []models.PlanItem{{Content: "read docs", Status: models.PlanItemInProgress}, {Content: "write", Status: models.PlanItemPending}}, plan)

	long := strings.Repeat("x", maxPlanItemChars+50)
	plan, err = ParseUpdatePlanArgs([]byte(`{"items":[{"content":"` + long + `","status":"completed"}]}`))
	require.NoError(t, err)
	assert.Len(t, []rune(plan[0].Content), maxPlanItemChars)

	for name, raw := range map[string]string{
		"bad json":       `{`,
		"empty content":  `{"items":[{"content":" ","status":"pending"}]}`,
		"unknown status": `{"items":[{"content":"x","status":"later"}]}`,
		"too many":       `{"items":[` + strings.TrimSuffix(strings.Repeat(`{"content":"x","status":"pending"},`, MaxPlanItems+1), ",") + `]}`,
	} {
		_, err := ParseUpdatePlanArgs([]byte(raw))
		assert.Error(t, err, name)
	}
}

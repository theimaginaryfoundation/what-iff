package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUtilityRequestValidate(t *testing.T) {
	t.Parallel()
	conversation := &ModelContext{}
	valid := map[string]UtilityRequest{
		"input":                  {Role: UtilityRoleSimple, Input: "hi"},
		"messages":               {Role: UtilityRoleSimple, Messages: []UtilityMessage{{Role: RoleDeveloper, Text: "a"}, {Role: RoleUser, Text: "b", ImageDataURL: "data:image/png;base64,AAAA"}}},
		"conversation alone":     {Role: UtilityRoleSimple, Context: conversation},
		"conversation and turns": {Role: UtilityRoleMemory, Context: conversation, Messages: []UtilityMessage{{Role: RoleUser, Text: "a"}}},
		"stored response":        {Role: UtilityRoleMemory, PreviousResponseID: "resp_1", Input: "a"},
		"empty input":            {Role: UtilityRoleSimple, Instructions: "x"},
	}
	for name, req := range valid {
		require.NoError(t, req.Validate(), name)
	}

	invalid := map[string]UtilityRequest{
		"unknown role":            {Role: "images", Input: "hi"},
		"no role":                 {Input: "hi"},
		"both conversations":      {Role: UtilityRoleMemory, Context: conversation, PreviousResponseID: "resp_1", Messages: []UtilityMessage{{Role: RoleUser, Text: "a"}}},
		"input and messages":      {Role: UtilityRoleSimple, Input: "a", Messages: []UtilityMessage{{Role: RoleUser, Text: "b"}}},
		"input with conversation": {Role: UtilityRoleSimple, Context: conversation, Input: "a"},
		"priority and flex":       {Role: UtilityRoleSimple, Input: "a", Priority: true, Flex: true},
		"negative cap":            {Role: UtilityRoleSimple, Input: "a", MaxOutputTokens: -1},
		"assistant turn":          {Role: UtilityRoleSimple, Messages: []UtilityMessage{{Role: RoleAssistant, Text: "a"}}},
		"image not a data URL":    {Role: UtilityRoleSimple, Messages: []UtilityMessage{{Role: RoleUser, Text: "a", ImageDataURL: "https://example.com/a.png"}}},
	}
	for name, req := range invalid {
		require.ErrorIs(t, req.Validate(), ErrInvalidUtilityRequest, name)
	}
}

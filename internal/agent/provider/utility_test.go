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
		"history and input":      {Role: UtilityRoleSimple, History: []UtilityMessage{{Role: UtilityMessageDeveloper, Text: "a"}}, Input: "b", InputImageDataURL: "data:image/png;base64,AAAA"},
		"conversation and input": {Role: UtilityRoleMemory, Context: conversation, Input: "a"},
		"stored response":        {Role: UtilityRoleMemory, PreviousResponseID: "resp_1", Input: "a"},
		"image alone":            {Role: UtilityRoleSimple, InputImageDataURL: "data:image/png;base64,AAAA"},
	}
	for name, req := range valid {
		require.NoError(t, req.Validate(), name)
	}

	invalid := map[string]UtilityRequest{
		"unknown role":         {Role: "images", Input: "hi"},
		"no role":              {Input: "hi"},
		"both conversations":   {Role: UtilityRoleMemory, Context: conversation, PreviousResponseID: "resp_1", Input: "a"},
		"unknown service tier": {Role: UtilityRoleSimple, Input: "a", ServiceTier: "fast"},
		"negative cap":         {Role: UtilityRoleSimple, Input: "a", MaxOutputTokens: -1},
		"assistant turn":       {Role: UtilityRoleSimple, History: []UtilityMessage{{Role: RoleAssistant, Text: "a"}}, Input: "b"},
		"nothing to ask":         {Role: UtilityRoleSimple, Instructions: "x"},
		"image not a data URL": {Role: UtilityRoleSimple, Input: "a", InputImageDataURL: "https://example.com/a.png"},
	}
	for name, req := range invalid {
		require.ErrorIs(t, req.Validate(), ErrInvalidUtilityRequest, name)
	}
}

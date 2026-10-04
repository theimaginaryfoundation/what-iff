package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/theimaginaryfoundation/what-iff/internal/models"
)

func runSubagentJSON(t *testing.T, chat *models.Chat, args string) runSubagentToolResult {
	t.Helper()
	// The Agent has no datastore: any attempt to load another personality, a ritual, or a ritual's
	// MCP servers would panic. A refusal therefore proves nothing was attached or looked up.
	a := &Agent{}
	out, err := a.runSubagentTool(context.Background(), &chatContext{model: "gpt-5-mini", chat: chat}, []byte(args))
	require.NoError(t, err)
	var res runSubagentToolResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	return res
}

func TestRunSubagentTool_RestrictedChatCannotPullInOtherAccountData(t *testing.T) {
	t.Parallel()
	own, other, ritual := uuid.New(), uuid.New(), uuid.New()
	restricted := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: own, MemorySensitivityLimit: models.MemorySensitivityPublic}

	for name, args := range map[string]string{
		"another personality":            `{"message":"hi","personality_id":"` + other.String() + `"}`,
		"a malformed personality id":     `{"message":"hi","personality_id":"not-a-uuid"}`,
		"skills (skill_ids)":             `{"message":"hi","skill_ids":["` + ritual.String() + `"]}`,
		"skills under the internal name": `{"message":"hi","ritual_ids":["` + ritual.String() + `"]}`,
		"both":                           `{"message":"hi","personality_id":"` + other.String() + `","skill_ids":["` + ritual.String() + `"]}`,
	} {
		res := runSubagentJSON(t, restricted, args)
		require.False(t, res.Success, name)
		require.Contains(t, res.Error, "restricted conversation", name)
	}

	// Blank values, and the chat's own personality, are no-ops and are not refused by the gate:
	// the call proceeds as far as resolving the (unknown) model.
	for name, args := range map[string]string{
		"no extras":       `{"message":"hi","model":"no-such-model"}`,
		"blank extras":    `{"message":"hi","model":"no-such-model","personality_id":" ","skill_ids":[" "]}`,
		"own personality": `{"message":"hi","model":"no-such-model","personality_id":"` + own.String() + `"}`,
	} {
		res := runSubagentJSON(t, restricted, args)
		require.Contains(t, res.Error, "not found", name)
	}

	// Control: an unrestricted chat is not subject to the gate (it reaches model resolution).
	open := &models.Chat{ID: uuid.New(), UserID: uuid.New(), PersonalityID: own}
	res := runSubagentJSON(t, open, `{"message":"hi","model":"no-such-model","skill_ids":[]}`)
	require.Contains(t, res.Error, "not found")
}

func TestSubagentToolContext_CarriesTheParentLimitAndTheOfferedSet(t *testing.T) {
	t.Parallel()
	uid := uuid.New()
	for _, limit := range []models.MemorySensitivity{models.MemorySensitivityPublic, models.MemorySensitivityPersonal} {
		c := subagentToolContext(uid, "m", limit, nil, map[string]struct{}{"mcp__s__t": {}})
		require.True(t, c.chat.MemoryRestricted(), "a sub-agent of a restricted chat is restricted")
		require.Equal(t, limit, c.chat.MemoryLimit())
		require.Equal(t, uid, c.chat.UserID)
		require.True(t, c.toolOffered("mcp__s__t"))
		require.False(t, c.toolOffered("recall"), "the sub-agent loop is offered only its MCP tools")
	}
	open := subagentToolContext(uid, "m", (&models.Chat{}).MemoryLimit(), nil, nil)
	require.False(t, open.chat.MemoryRestricted())
	require.False(t, open.toolOffered("anything"), "no offered set means no tools")
}

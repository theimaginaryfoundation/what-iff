package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/theimaginaryfoundation/what-iff/internal/agent/mcpclient"
)

func TestMCPAppRecordAndTake(t *testing.T) {
	c := &chatContext{}
	assert.Nil(t, c.takeMCPApp("x"))
	server := uuid.New()
	c.recordMCPApp("call_1", mcpAppForResult(mcpclient.CallToolResult{ServerID: server, UIResourceURI: "ui://a/b", ToolName: "show", Raw: json.RawMessage(`{"content":[]}`)}))
	app := c.takeMCPApp("call_1")
	if assert.NotNil(t, app) {
		assert.Equal(t, server, app.ServerID)
		assert.Equal(t, "ui://a/b", app.ResourceURI)
		assert.JSONEq(t, `{"content":[]}`, string(app.Result))
	}
	assert.Nil(t, c.takeMCPApp("call_1"), "taken once")
}

func TestMCPAppForResult_DropsOversizedResult(t *testing.T) {
	big := json.RawMessage(`{"text":"` + strings.Repeat("x", mcpclient.MaxAppResultBytes) + `"}`)
	app := mcpAppForResult(mcpclient.CallToolResult{UIResourceURI: "ui://a/b", Raw: big})
	assert.Empty(t, app.Result)
	assert.Equal(t, "ui://a/b", app.ResourceURI)
}

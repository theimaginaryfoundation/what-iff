package tools

const LoadMCPToolsDescription = `Load MCP tools from one connector into this chat's active toolset.

The connectors in this chat and their tool names are listed in your context (and by list(kind="mcp_servers")). Loaded tools can be called right away in this same turn, and stay available in later turns of this chat until you unload them.

Inputs:
- mcp_server_id (required): connector UUID (the connector's name also works)
- tools (required): array of MCP tool names to load.

Notes:
- You can pass canonical full MCP names (mcp__<connector>__<tool>) or connector-local names.
- Pass "all" or "*" in tools to load every discoverable tool from that connector.`

const UnloadMCPToolsDescription = `Unload MCP tools from this chat's active toolset.

Loaded MCP tools persist across turns; call this to remove tools you no longer need.

Inputs:
- mcp_server_id (optional): connector UUID to target. Required when unloading specific tool names.
- tools (required): array of names to unload, or include "all"/"*" to clear everything.

Behavior:
- tools includes "all" or "*" + mcp_server_id set: unload all tools for that connector.
- tools includes "all" or "*" + no mcp_server_id: unload all loaded MCP tools across all connectors in this chat.`

var LoadMCPToolsToolSpec = FunctionToolSpec{
	Name:        "load_mcp_tools",
	Description: LoadMCPToolsDescription,
	Properties: map[string]interface{}{
		"mcp_server_id": map[string]interface{}{
			"type":        "string",
			"description": "UUID of the MCP connector to load tools from (from list kind=\"mcp_servers\").",
		},
		"tools": map[string]interface{}{
			"type":        "array",
			"description": "MCP tool names to load for this chat, or [\"all\"] / [\"*\"] for every discoverable tool.",
			"items": map[string]interface{}{
				"type": "string",
			},
			"minItems": 1,
		},
	},
	Required: []string{"mcp_server_id", "tools"},
}

var UnloadMCPToolsToolSpec = FunctionToolSpec{
	Name:        "unload_mcp_tools",
	Description: UnloadMCPToolsDescription,
	Properties: map[string]interface{}{
		"mcp_server_id": map[string]interface{}{
			"type":        "string",
			"description": "Optional UUID of one MCP connector to target. Omit only when tools includes \"all\" or \"*\".",
		},
		"tools": map[string]interface{}{
			"type":        "array",
			"description": "Tool names to unload, or [\"all\"] / [\"*\"] to unload everything (connector-scoped when mcp_server_id is set; global otherwise).",
			"items": map[string]interface{}{
				"type": "string",
			},
			"minItems": 1,
		},
	},
	Required: []string{"tools"},
}

package tools

// Tool name constants used for disabled-tool matching.
const (
	ToolNameWebSearch = "web_search"
)

// Short descriptions of the web_search toggle for the /api/tools listing and the first-chat
// greeting. Full FunctionToolSpec descriptions are written for the model, not the user. The
// copy depends on which web search is running (ADR 0x021): first-party search can also read
// pages, while vendor-native search depends on the chat's model.
const (
	WebSearchDescriptionFirstParty = "Look up current information (news, scores, prices, releases) and read web pages you share or it finds."
	WebSearchDescriptionNative     = "Look up current information on the web. Works with models whose provider has built-in search."
)

// User guides for the web_search tooltip, by web search mode (see FunctionToolDefinition.UserGuide).
// Only first-party search takes recency and site options and can read pages.
const (
	WebSearchGuideFirstParty = "Ask for recent results only (past day, week, month or year), or limit it to certain sites. It can also read a page you paste in, or one it finds."
	WebSearchGuideNative     = "Uses the model provider's own web search, so it's only available on models whose provider has one. Ask it to look something up or check the latest news."
)

// WebSearchToggleGuide returns the user guide for the active web search.
func WebSearchToggleGuide(firstParty bool) string {
	if firstParty {
		return WebSearchGuideFirstParty
	}
	return WebSearchGuideNative
}

// WebSearchToggleDescription returns the user-facing web_search description for the active
// web search: first-party (PARALLEL_API_KEY set) or vendor-native.
func WebSearchToggleDescription(firstParty bool) string {
	if firstParty {
		return WebSearchDescriptionFirstParty
	}
	return WebSearchDescriptionNative
}

// FunctionToolSpec captures provider-agnostic function tool metadata.
// Both OpenAI and Claude tool registration should use these shared specs
// to avoid drift in names, descriptions, and parameter schemas.
type FunctionToolSpec struct {
	Name        string
	Description string
	Properties  map[string]interface{}
	Required    []string
}

const GenerateImageToolDescription = `Generate one or more images and attach them to the assistant's NEXT message in this chat.

Use this tool when the user asks for images (e.g., logos, memes, illustrations) or when an image would significantly improve the response.
Be selective when deciding to generate images on your own; prefer generating only when a visual adds clear value (clarity/comprehension).
Respect safety/content policies. If the request is disallowed, explain instead of calling this tool.

Notes:
- This tool returns metadata only. The actual image bytes will be attached to the assistant message automatically.
- Prefer concise but specific prompts (subject, style, composition, colors, lighting). Include exact text if the image should render text.
- Default count is 1 unless the user asks for variations/options.
- Quality controls cost and detail. Choose the level appropriate to the request; higher quality yields more detail.
- Use aspect_ratio to match the subject: square for icons/avatars/logos, landscape for scenes/banners, portrait for characters/posters. Defaults to square.
- To create multiple variations, either set count > 1 or call this tool multiple times with different prompts.`

const CreateAgentJobToolDescription = `Schedule a future reminder / follow-up / check-in / task for the current user.

This tool schedules exactly ONE job per tool call.

When to use
Call this tool when BOTH are true:

1) The user refers to a future time (e.g., "in an hour", "tomorrow", "next week", a clock time, or "every day at 7am"), AND
2) They are asking you to act at that time (remind / ping / message / follow up / check in / ask them something).

Examples that SHOULD trigger this tool (even if "agent job" or "schedule" isn't mentioned):
- "Remind me in an hour to stretch."
- "Send me a message tomorrow about the report."
- "In two hours, check in whether I've done X."
- "Every day at 7am, remind me to do Y."
- "Once a week on Monday, ask me if I finished Z."

If the request is vague but clearly about a future reminder (e.g., "remind me about this later", "follow up at some point"), ask the user to pick a time before calling the tool.

If the what of the reminder is unclear (e.g., "remind me later about this" with no context), briefly clarify or infer a short summary from recent conversation, and include that in the prompt.

If the user is only describing plans and not asking you to act later (e.g., "I should do this in an hour", "Tomorrow I'll work on Y"), do NOT schedule unless they confirm they want a reminder.

Do NOT use this tool for general planning/scheduling advice (e.g., "Help me plan my week", "What's a good schedule for X?") unless the user then asks for a specific reminder.

Do NOT use this tool for reminders that are only for you or other agents; it is only for reminders sent to the user.

When a reminder you created fires and the user replies with "remind me again in X" or "snooze this to Y", use this tool again to schedule the new reminder.

Multiple or recurring times
If the user clearly requests a recurring pattern (e.g., "every weekday at 8am", "once a month on the 1st"), you may pass that natural language pattern in schedule_input.
Minimum recurring interval is 5 minutes:
- Do NOT create recurring schedules that run more often than every 5 minutes.
- If the user asks for something more frequent (e.g., every 1-4 minutes, every 2 minutes in a time window), ask them to choose 5 minutes or longer before calling this tool.

If they mention more than one distinct time (e.g., "remind me tomorrow and again next week"), either:
- choose the clearest intended schedule if it’s obvious, OR
- ask a brief clarification before scheduling.

If a time phrase like "at 7" or "tonight" is ambiguous relative to the current time, you may ask the user to clarify "today or tomorrow?" before scheduling.

What to send
When you decide to schedule, you must provide:

title – Short, human-readable summary of the reminder.
- Keep titles brief (under ~10 words), neutral tone, no emojis.

schedule_input – The user's original natural language timing, copied as-is or very close.

prompt – A clear instruction that will be injected as the user message when the job fires. It should:
- Remind the assistant what was requested, and
- Prompt an appropriate follow-up question or action.
- 1–3 sentences, clear and direct.

skill_ids (optional) – Array of skill UUIDs to attach. When the job fires, each skill's content is prepended to the prompt and its MCP servers are available. Use list (kind="skills") to discover available skills.

model_override (optional) – UUID or name of the model to use when the job runs, overriding the chat default. Use list (kind="models") to discover options.

use_current_thread (optional, boolean) – When true, run the job in the current conversation thread. When false or omitted, the job creates a dedicated thread on first run (default).

Example:
User: "Remind me in an hour to take a break from coding." →
{
 "title": "Break reminder from coding",
 "schedule_input": "in an hour",
 "prompt": "Reminder from your earlier request: you asked to be reminded to take a break from coding now. Ask how the coding is going and help them pause or switch tasks."
}

Schedule times are interpreted using the user's saved timezone (no timezone argument needed).
`

var UpdateScratchpadToolSpec = FunctionToolSpec{
	Name:        "update_scratchpad",
	Description: UpdateScratchpadDescription,
	Properties: map[string]interface{}{
		"content": map[string]interface{}{
			"type":        "string",
			"description": "The new content for the scratchpad. This will completely replace the existing scratchpad content, so include everything you want to remember.",
		},
	},
	Required: []string{"content"},
}

var CreateMemoryToolSpec = FunctionToolSpec{
	Name:        "create_memory",
	Description: CreateMemoryDescription,
	Properties: map[string]interface{}{
		"content": map[string]interface{}{
			"type":        "string",
			"description": "The content of the memory to store. Be specific and concise. Focus on facts, preferences, or important context.",
		},
		"scope": map[string]interface{}{
			"type":        "string",
			"description": "The scope of the memory. Use 'User' for information that applies globally across all conversations (e.g., preferences, personal facts). Use 'Chat' for information specific to this conversation (e.g., project-specific context, conversation goals).",
			"enum":        []string{MemoryScopeUser, MemoryScopeChat},
		},
	},
	Required: []string{"content", "scope"},
}

var GenerateImageToolSpec = FunctionToolSpec{
	Name:        "generate_image",
	Description: GenerateImageToolDescription,
	Properties: map[string]interface{}{
		"prompt": map[string]interface{}{
			"type":        "string",
			"description": "A high-quality prompt for the image model. Be specific about style and composition.",
		},
		"count": map[string]interface{}{
			"type":        "integer",
			"description": "Number of images to generate (1-4).",
			"minimum":     1,
			"maximum":     4,
		},
		"quality": map[string]interface{}{
			"type":        "string",
			"description": "Image quality level. low is cheapest and fastest; medium and high add progressively more detail.",
			"enum":        []string{"low", "medium", "high"},
		},
		"aspect_ratio": map[string]interface{}{
			"type":        "string",
			"description": "Image aspect ratio. square is 1024x1024, landscape is 1536x1024, portrait is 1024x1536. Defaults to square.",
			"enum":        []string{"square", "landscape", "portrait"},
		},
		"filename_prefix": map[string]interface{}{
			"type":        "string",
			"description": "Optional prefix to use for image filenames (e.g., 'logo_concept').",
		},
	},
	Required: []string{"prompt"},
}

var CreateAgentJobToolSpec = FunctionToolSpec{
	Name:        "create_agent_job",
	Description: CreateAgentJobToolDescription,
	Properties: map[string]interface{}{
		"title": map[string]interface{}{
			"type":        "string",
			"description": "Optional short title for the scheduled job (e.g., 'Email follow-up reminder').",
		},
		"schedule_input": map[string]interface{}{
			"type":        "string",
			"description": "The schedule in natural language exactly as the user requested (e.g., 'in an hour', 'every weekday at 8 AM'). For recurring schedules, do not provide patterns more frequent than every 5 minutes.",
		},
		"prompt": map[string]interface{}{
			"type":        "string",
			"description": "The prompt to run when the job fires. This will be used as the injected user message to kick off execution in the job chat.",
		},
		"skill_ids": map[string]interface{}{
			"type":        "array",
			"description": "Optional array of skill UUIDs to attach to the job. When the job fires, each skill's content is appended to the prompt and its MCP servers are available. Use list (kind=\"skills\") to discover available skills.",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
		"model_override": map[string]interface{}{
			"type":        "string",
			"description": "Optional: UUID or name of the model to use when the job runs, overriding the chat default.",
		},
		"use_current_thread": map[string]interface{}{
			"type":        "boolean",
			"description": "When true, run the job in the current conversation thread. When false or omitted, create a dedicated thread on first run (default).",
		},
	},
	Required: []string{"schedule_input", "prompt"},
}

const RunSubagentToolDescription = `Synchronously run a focused subagent call and return only its output.

Inputs:
- message (required): The exact prompt for the subagent.
- personality_id (optional): UUID of the personality to use; defaults to the current chat personality.
- model (optional): UUID or name (case-insensitive) of the model to use; defaults to the current chat model. Use list (kind="models") to see available options.
- skill_ids (optional): Array of skill UUIDs whose content should be appended to the subagent message. Use list (kind="skills") to discover available skills.

Context rules:
- The subagent receives only system prompt + scratchpad from the selected personality and the provided message.
- Chat history, checkpoint summary, and memory context are excluded.
- This tool returns the subagent output as the tool result.
- This tool does not directly trigger scratchpad updates, memory creation, or thread summarization.`

var RunSubagentToolSpec = FunctionToolSpec{
	Name:        "run_subagent",
	Description: RunSubagentToolDescription,
	Properties: map[string]interface{}{
		"message": map[string]interface{}{
			"type":        "string",
			"description": "The prompt to send to the subagent.",
		},
		"personality_id": map[string]interface{}{
			"type":        "string",
			"description": "Optional UUID of the personality to use for this subagent run.",
		},
		"model": map[string]interface{}{
			"type":        "string",
			"description": "Optional UUID or name (case-insensitive) of the model to use for this subagent run. Use list (kind=\"models\") to discover available models.",
		},
		"skill_ids": map[string]interface{}{
			"type":        "array",
			"description": "Optional array of skill UUIDs whose content will be appended to the subagent message. Use list (kind=\"skills\") to discover available skills.",
			"items": map[string]interface{}{
				"type": "string",
			},
		},
	},
	Required: []string{"message"},
}

// ToolNameFetchPage is the first-party page-reading tool that pairs with web_search (ADR 0x021).
const ToolNameFetchPage = "fetch_page"

// WebSearchFunctionToolSpec is the first-party web search tool (ADR 0x021). It reuses the
// web_search name so the user's web search toggle and saved tool-call history stay the same
// whichever backend runs; the agent never offers it alongside a vendor-native web_search.
var WebSearchFunctionToolSpec = FunctionToolSpec{
	Name: ToolNameWebSearch,
	Description: "Search the web for current or factual information you don't already know. " +
		"Returns a short list of results with title, URL, snippet and published date (often missing). " +
		"Prefer the snippets; use fetch_page only on the one or two URLs your answer depends on. " +
		"Cite the URLs you rely on.",
	Properties: map[string]interface{}{
		"query": map[string]interface{}{
			"type":        "string",
			"description": "A concise search query, as you would type it into a search engine.",
		},
		"objective": map[string]interface{}{
			"type":        "string",
			"description": "Optional: one sentence on what you are trying to find out, used to rank and excerpt results.",
		},
		"max_results": map[string]interface{}{
			"type":        "integer",
			"description": "Number of results to return (1-10). Defaults to 5.",
			"minimum":     1,
			"maximum":     10,
		},
		"recency": map[string]interface{}{
			"type": "string",
			"enum": []string{"day", "week", "month", "year"},
			"description": "Optional: drop pages published before the last day, week, month or year. " +
				"Use it for news, scores, prices, releases and anything else asked about as recent. " +
				"Pages with no published date are not filtered out, so check the published field before treating a result as recent.",
		},
		"published_after": map[string]interface{}{
			"type": "string",
			"description": "Optional: like recency but with an exact cutoff date, YYYY-MM-DD. " +
				"If both are given, the later cutoff applies. Undated pages can still appear.",
		},
		"include_domains": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional: only return results from these domains, e.g. [\"espn.com\"] (at most 10). This is a strict filter.",
		},
		"exclude_domains": map[string]interface{}{
			"type":        "array",
			"items":       map[string]interface{}{"type": "string"},
			"description": "Optional: never return results from these domains (at most 10).",
		},
	},
	Required: []string{"query"},
}

// FetchPageToolSpec reads one web page through the search provider's extract API.
var FetchPageToolSpec = FunctionToolSpec{
	Name: ToolNameFetchPage,
	Description: "Read the text of one web page, usually a URL returned by web_search. " +
		"Give an objective to get only the relevant excerpts instead of the whole page. " +
		"It can fail on JavaScript-heavy sites, some PDFs, or blocked and rate-limited hosts; the error says why. " +
		"On failure, tell the user what went wrong rather than retrying the same URL.",
	Properties: map[string]interface{}{
		"url": map[string]interface{}{
			"type":        "string",
			"description": "The full http(s) URL to read.",
		},
		"objective": map[string]interface{}{
			"type":        "string",
			"description": "Optional: what you want from the page; returns focused excerpts instead of the full text.",
		},
	},
	Required: []string{"url"},
}

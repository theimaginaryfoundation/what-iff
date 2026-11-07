export interface ToolCall {
  id: string;
  chat_message_id: string;
  tool_name: string;
  tool_input: string;
  tool_output: string;
  tool_error: string;
  /** Advanced-loop step (1-based) the call ran in; absent on Standard turns. */
  step?: number;
  /** Set when the call's MCP tool has an MCP App (ADR 0x042): the chat renders its view. */
  app?: ToolCallApp | null;
  created_at: string;
  updated_at: string;
}
/** Lifecycle of a tool call; saved calls are always settled, live (in-flight) ones start as running. */
export type ToolCallStatus = 'running' | 'complete' | 'error';

/** A tool call as rendered: saved calls leave status unset, live ones carry it. */
export type ToolCallView = ToolCall & { readonly status?: ToolCallStatus };

/** Where an MCP App's view comes from (ADR 0x042). */
export interface ToolCallApp {
  server_id: string;
  resource_uri: string;
  tool_name: string;
}

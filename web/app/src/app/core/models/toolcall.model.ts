export interface ToolCall {
  id: string;
  chat_message_id: string;
  tool_name: string;
  tool_input: string;
  tool_output: string;
  tool_error: string;
  created_at: string;
  updated_at: string;
}
/** Lifecycle of a tool call; saved calls are always settled, live (in-flight) ones start as running. */
export type ToolCallStatus = 'running' | 'complete' | 'error';

/** A tool call as rendered: saved calls leave status unset, live ones carry it. */
export type ToolCallView = ToolCall & { readonly status?: ToolCallStatus };

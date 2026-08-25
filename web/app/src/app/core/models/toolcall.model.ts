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
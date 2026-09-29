import { parseChatTurnToolCalls } from './assistant-turn';

describe('parseChatTurnToolCalls', () => {
  it('reads the tool timeline from a chat_message progress payload', () => {
    const calls = [{ id: 'c1', name: 'recall', status: 'running', round: 0, started_at: 't0' }];
    expect(parseChatTurnToolCalls(JSON.stringify({ tool_calls: calls }))).toEqual(calls);
  });

  it('ignores missing, malformed or foreign progress payloads', () => {
    expect(parseChatTurnToolCalls(undefined)).toBeUndefined();
    expect(parseChatTurnToolCalls('')).toBeUndefined();
    expect(parseChatTurnToolCalls('{not json')).toBeUndefined();
    // e.g. a chat_import payload
    expect(parseChatTurnToolCalls(JSON.stringify({ phase: 'importing', total: 3 }))).toBeUndefined();
    expect(parseChatTurnToolCalls(JSON.stringify({ tool_calls: 'nope' }))).toBeUndefined();
  });
});

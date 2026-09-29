import { parseChatTurnProgress, parseChatTurnToolCalls } from './assistant-turn';

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

describe('parseChatTurnProgress', () => {
  it('reads the phase alongside the tool timeline', () => {
    expect(parseChatTurnProgress(JSON.stringify({ phase: 'loading_memories', tool_calls: [] }))).toEqual({
      phase: 'loading_memories',
      toolCalls: [],
    });
    expect(parseChatTurnProgress(JSON.stringify({ phase: 'inference', tool_calls: [] }))?.phase).toBe('inference');
  });

  it('leaves the phase unset when the payload has none or an unknown one', () => {
    expect(parseChatTurnProgress(JSON.stringify({ tool_calls: [] }))).toEqual({ phase: undefined, toolCalls: [] });
    // A chat_import payload's phase is not a chat turn phase.
    expect(parseChatTurnProgress(JSON.stringify({ phase: 'importing', total: 3 }))).toBeUndefined();
    expect(parseChatTurnProgress('null')).toBeUndefined();
  });
});

import { CHAT_TURN_WAITING_STATES } from '../../core/models/job.model';
import { chatTurnWaitingLabel, parseChatTurnToolCalls, parseChatTurnWaiting } from './assistant-turn';

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

describe('parseChatTurnWaiting', () => {
  it('reads what a queued turn waits on', () => {
    for (const waiting of CHAT_TURN_WAITING_STATES) {
      expect(parseChatTurnWaiting(JSON.stringify({ tool_calls: [], waiting_on: waiting }))).toBe(waiting);
    }
  });

  it('is null once the turn runs and for unreadable or unknown payloads', () => {
    expect(parseChatTurnWaiting(JSON.stringify({ tool_calls: [] }))).toBeNull();
    expect(parseChatTurnWaiting(undefined)).toBeNull();
    expect(parseChatTurnWaiting('{not json')).toBeNull();
    expect(parseChatTurnWaiting(JSON.stringify({ tool_calls: [], waiting_on: 'nonsense' }))).toBeNull();
  });

  it('labels the wait', () => {
    expect(chatTurnWaitingLabel('reply')).toBe('Waiting for the previous reply…');
  });
});

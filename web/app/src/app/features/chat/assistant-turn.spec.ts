import { CHAT_TURN_WAITING_STATES } from '../../core/models/job.model';
import { chatTurnWaitingLabel, parseChatTurnAdvancedProgress, parseChatTurnToolCalls, parseChatTurnWaiting } from './assistant-turn';

describe('parseChatTurnAdvancedProgress', () => {
    it('reads the step and plan of an Advanced turn, dropping malformed plan items', () => {
        const progress = JSON.stringify({
            tool_calls: [],
            step: 3,
            plan: [
                { content: 'Search', status: 'completed' },
                { content: 'Summarize', status: 'in_progress' },
                { content: '', status: 'pending' },
                { content: 'Bogus', status: 'blocked' },
                null,
            ],
        });
        expect(parseChatTurnAdvancedProgress(progress)).toEqual({
            step: 3,
            plan: [
                { content: 'Search', status: 'completed' },
                { content: 'Summarize', status: 'in_progress' },
            ],
        });
    });

    it('is null for a Standard turn or an unreadable payload', () => {
        expect(parseChatTurnAdvancedProgress(JSON.stringify({ tool_calls: [] }))).toBeNull();
        expect(parseChatTurnAdvancedProgress(JSON.stringify({ step: 0 }))).toBeNull();
        expect(parseChatTurnAdvancedProgress('not json')).toBeNull();
        expect(parseChatTurnAdvancedProgress('null')).toBeNull();
        expect(parseChatTurnAdvancedProgress(undefined)).toBeNull();
    });

    it('treats a missing plan as empty', () => {
        expect(parseChatTurnAdvancedProgress(JSON.stringify({ step: 1 }))).toEqual({ step: 1, plan: [] });
    });
});

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

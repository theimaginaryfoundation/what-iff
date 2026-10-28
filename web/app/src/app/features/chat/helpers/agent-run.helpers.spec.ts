import { ToolCall } from '../../../core/models/toolcall.model';
import { agentRunStopLabel, agentRunView, normalizePlanItems } from './agent-run.helpers';

describe('agent-run helpers', () => {
  describe('normalizePlanItems', () => {
    it('drops items without content and, by default, items with an unknown status', () => {
      expect(
        normalizePlanItems([
          { content: 'Search', status: 'completed' },
          { content: '  ', status: 'pending' },
          { content: 'Mystery', status: 'blocked' },
          null,
          'nope',
        ]),
      ).toEqual([{ content: 'Search', status: 'completed' }]);
    });

    it('shows an unknown status as the given fallback', () => {
      expect(normalizePlanItems([{ content: 'Mystery', status: 'blocked' }], 'pending')).toEqual([
        { content: 'Mystery', status: 'pending' },
      ]);
    });

    it('returns an empty plan for a non-array', () => {
      expect(normalizePlanItems(undefined)).toEqual([]);
      expect(normalizePlanItems({ content: 'x' })).toEqual([]);
    });
  });

  describe('agentRunStopLabel', () => {
    it.each([
      ['step_limit', 'Stopped: step limit reached'],
      ['duration_limit', 'Stopped: time limit reached'],
      ['quota', 'Stopped: credit limit reached'],
      ['cancelled', 'Stopped: cancelled'],
      ['failed', 'Stopped: failed'],
      ['stalled', 'Stopped: no further progress'],
      ['something_new', 'Stopped before finishing'],
      ['toString', 'Stopped before finishing'],
    ])('labels %s', (reason, label) => {
      expect(agentRunStopLabel(reason)).toBe(label);
    });

    it('has no label for a finished run or a missing reason', () => {
      expect(agentRunStopLabel('done')).toBeNull();
      expect(agentRunStopLabel(undefined)).toBeNull();
      expect(agentRunStopLabel('')).toBeNull();
    });
  });

  describe('agentRunView', () => {
    const call = (id: string, tool_name: string, tool_error = ''): ToolCall => ({
      id,
      chat_message_id: 'm1',
      tool_name,
      tool_input: '{}',
      tool_output: '{}',
      tool_error,
      step: 1,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    });

    it('is null when there is no run or nothing to show', () => {
      expect(agentRunView(undefined)).toBeNull();
      expect(agentRunView(null)).toBeNull();
      expect(agentRunView({})).toBeNull();
      expect(agentRunView({ version: 1, steps: [{ step: 1, events: [{ kind: 'text', text: '  ' }] }], end_reason: 'done' })).toBeNull();
    });

    it("keeps each step's events in order, trims notes and resolves tool calls by id", () => {
      const search = call('tc-1', 'web_search');
      const fetch = call('tc-2', 'fetch_url', 'timeout');
      const view = agentRunView(
        {
          version: 1,
          steps: [
            {
              step: 1,
              events: [
                { kind: 'reasoning', text: ' Think. ' },
                { kind: 'text', text: 'Let me search.' },
                { kind: 'tool_call', tool_call_id: 'tc-1' },
                { kind: 'tool_call', tool_call_id: 'tc-2' },
              ],
            },
            { step: 2 },
          ],
          end_reason: 'done',
        },
        [search, fetch],
      );
      expect(view).toEqual({
        steps: [
          {
            step: 1,
            events: [
              { kind: 'reasoning', text: 'Think.' },
              { kind: 'text', text: 'Let me search.' },
              { kind: 'tool_call', toolCall: search, name: 'web search', failed: false },
              { kind: 'tool_call', toolCall: fetch, name: 'fetch url', failed: true },
            ],
          },
        ],
        plan: [],
        stopLabel: null,
      });
    });

    it('skips unknown kinds and tool calls the message does not carry, dropping steps left empty', () => {
      const view = agentRunView(
        {
          version: 1,
          steps: [
            {
              step: 1,
              events: [
                { kind: 'summary', text: 'Odd kind' },
                { kind: 'tool_call', tool_call_id: 'missing' },
              ],
            },
            { step: 2, events: [{ kind: 'tool_call' }, { kind: 'text', text: 'Kept.' }] },
          ],
        },
        [call('tc-1', 'web_search')],
      );
      expect(view?.steps).toEqual([{ step: 2, events: [{ kind: 'text', text: 'Kept.' }] }]);
    });

    it('renders the known parts of a newer version', () => {
      const view = agentRunView({
        version: 2,
        steps: [
          {
            step: 1,
            events: [
              { kind: 'image', text: 'x' },
              { kind: 'reasoning', text: 'Still here.' },
            ],
          },
        ],
        end_reason: 'quota',
      });
      expect(view).toEqual({
        steps: [{ step: 1, events: [{ kind: 'reasoning', text: 'Still here.' }] }],
        plan: [],
        stopLabel: 'Stopped: credit limit reached',
      });
    });

    it('carries the plan and a stop label even without notes', () => {
      expect(agentRunView({ plan: [{ content: 'Read', status: 'in_progress' }], end_reason: 'step_limit' })).toEqual({
        steps: [],
        plan: [{ content: 'Read', status: 'in_progress' }],
        stopLabel: 'Stopped: step limit reached',
      });
      expect(agentRunView({ end_reason: 'failed' })?.stopLabel).toBe('Stopped: failed');
    });
  });
});

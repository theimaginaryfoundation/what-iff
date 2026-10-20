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
    it('is null when there is no run or nothing to show', () => {
      expect(agentRunView(undefined)).toBeNull();
      expect(agentRunView(null)).toBeNull();
      expect(agentRunView({})).toBeNull();
      expect(agentRunView({ steps: [{ step: 1, notes: [{ kind: 'text', text: '  ' }] }], end_reason: 'done' })).toBeNull();
    });

    it('keeps steps with notes, trims notes and reads unknown kinds as text', () => {
      const view = agentRunView({
        steps: [
          {
            step: 1,
            notes: [
              { kind: 'reasoning', text: ' Think. ' },
              { kind: 'text', text: 'Let me search.' },
            ],
          },
          { step: 2 },
          { step: 3, notes: [{ kind: 'summary', text: 'Odd kind' }] },
        ],
        end_reason: 'done',
      });
      expect(view).toEqual({
        steps: [
          {
            step: 1,
            notes: [
              { kind: 'reasoning', text: 'Think.' },
              { kind: 'text', text: 'Let me search.' },
            ],
          },
          { step: 3, notes: [{ kind: 'text', text: 'Odd kind' }] },
        ],
        plan: [],
        stopLabel: null,
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

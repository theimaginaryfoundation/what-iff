import { ContextBreakdown } from '../../../core/models/message.model';
import { contextBudgetView } from './context-budget.helpers';

const base: ContextBreakdown = {
  segments: [{ kind: 'history_turn', segments: 1, tokens: 1000, cacheable: false }],
  total_tokens: 1000,
  budget_tokens: 30000,
  captured_at: '2026-11-03T12:00:00Z',
};

describe('contextBudgetView', () => {
  it('reads a Standard turn against the checkpoint budget', () => {
    const view = contextBudgetView({ ...base, agent_loop: 'standard', checkpoint_input_tokens: 42000 }, 42000);
    expect(view.kind).toBe('checkpoint');
    expect(view.main).toEqual({ used: 42000, ceiling: 30000, over: true, overBy: 12000, usedPct: 140 });
    expect(view.checkpoint).toBeNull();
  });

  it('reads a snapshot from before the run fields as a Standard one', () => {
    const view = contextBudgetView(base, 5720);
    expect(view.kind).toBe('checkpoint');
    expect(view.main.ceiling).toBe(30000);
    expect(view.main.over).toBe(false);
  });

  // #397: "60k of 30k" — an Advanced run's final call carries the run's own work, so it is read
  // against the run's limit, and the checkpoint is read on the run's first call as the server does.
  it('reads an Advanced run against its own limit, with the checkpoint on its first call', () => {
    const view = contextBudgetView(
      {
        ...base,
        total_tokens: 60000,
        agent_loop: 'advanced',
        run_budget_tokens: 140000,
        checkpoint_input_tokens: 20000,
        run_compactions: 1,
      },
      60000,
    );
    expect(view.kind).toBe('run');
    expect(view.main).toEqual({ used: 60000, ceiling: 140000, over: false, overBy: 0, usedPct: 43 });
    expect(view.checkpoint).toEqual({ used: 20000, ceiling: 30000, over: false, overBy: 0, usedPct: 67 });
    expect(view.compactions).toBe(1);
  });

  it('never compares an Advanced total to the checkpoint budget, even without a run limit', () => {
    const view = contextBudgetView({ ...base, total_tokens: 60000, agent_loop: 'advanced' }, 60000);
    expect(view.kind).toBe('run');
    expect(view.main.ceiling).toBe(0);
    expect(view.main.over).toBe(false);
    expect(view.checkpoint).toBeNull();
  });

  it('flags an Advanced run past its limit and a checkpoint past its budget separately', () => {
    const view = contextBudgetView({ ...base, agent_loop: 'advanced', run_budget_tokens: 50000, checkpoint_input_tokens: 31000 }, 62000);
    expect(view.main.over).toBe(true);
    expect(view.main.overBy).toBe(12000);
    expect(view.checkpoint?.over).toBe(true);
  });
});

import {
  effectiveReasoningEffort,
  parseAgentLoop,
  parseReasoningEffort,
  reasoningEffortLabel,
  reasoningEffortOptions,
  resolveEffectiveTurnSettings,
} from './turn-settings.model';

describe('reasoningEffortOptions', () => {
  it('offers the choices each kind of model supports', () => {
    const values = (support: Parameters<typeof reasoningEffortOptions>[0]) => reasoningEffortOptions(support).map(o => o.value);
    expect(values('optional')).toEqual(['off', 'low', 'medium', 'high']);
    expect(values('always')).toEqual(['low', 'medium', 'high']);
    expect(reasoningEffortOptions('toggle')).toEqual([
      { value: 'off', label: 'Off' },
      { value: 'medium', label: 'On' },
    ]);
    expect(values('none')).toEqual([]);
    expect(values(undefined)).toEqual([]);
  });
});

describe('effectiveReasoningEffort', () => {
  it('maps a stored effort onto what the model runs, like the server', () => {
    expect(effectiveReasoningEffort('optional', 'off')).toBe('off');
    expect(effectiveReasoningEffort('optional', 'high')).toBe('high');
    expect(effectiveReasoningEffort('toggle', 'low')).toBe('medium');
    expect(effectiveReasoningEffort('toggle', 'off')).toBe('off');
    expect(effectiveReasoningEffort('always', 'off')).toBe('low');
    expect(effectiveReasoningEffort('always', 'high')).toBe('high');
  });

  it('is null when not set or when the model has no control', () => {
    expect(effectiveReasoningEffort('optional', null)).toBeNull();
    expect(effectiveReasoningEffort('none', 'high')).toBeNull();
    expect(effectiveReasoningEffort(undefined, 'high')).toBeNull();
  });
});

describe('reasoningEffortLabel', () => {
  it('labels the effective effort, with On/Off for toggle models', () => {
    expect(reasoningEffortLabel('optional', 'medium')).toBe('Medium');
    expect(reasoningEffortLabel('toggle', 'high')).toBe('On');
    expect(reasoningEffortLabel('toggle', 'off')).toBe('Off');
    expect(reasoningEffortLabel('always', 'off')).toBe('Low');
    expect(reasoningEffortLabel('optional', null)).toBeNull();
    expect(reasoningEffortLabel('none', 'high')).toBeNull();
  });
});

describe('parseReasoningEffort / parseAgentLoop', () => {
  it('reads select values, treating the empty option and unknown values as not set', () => {
    expect(parseReasoningEffort('low')).toBe('low');
    expect(parseReasoningEffort('')).toBeNull();
    expect(parseReasoningEffort('max')).toBeNull();
    expect(parseAgentLoop('advanced')).toBe('advanced');
    expect(parseAgentLoop('')).toBeNull();
    expect(parseAgentLoop(undefined)).toBeNull();
  });
});

describe('resolveEffectiveTurnSettings', () => {
  it('uses the mode value over the thread value, per setting', () => {
    const resolved = resolveEffectiveTurnSettings({
      thread: { reasoning_effort: 'low', agent_loop: 'advanced' },
      mode: { reasoning_effort: 'high', agent_loop: null },
    });
    expect(resolved.reasoningEffort).toEqual({ value: 'high', source: 'mode' });
    expect(resolved.agentLoop).toEqual({ value: 'advanced', source: 'thread' });
  });

  it('follows job → mode → thread → personality → account, per setting', () => {
    const levels = {
      job: { reasoning_effort: 'off' as const },
      mode: { agent_loop: 'advanced' as const },
      thread: { reasoning_effort: 'low' as const, agent_loop: 'standard' as const },
      personality: { reasoning_effort: 'medium' as const, agent_loop: 'standard' as const },
      account: { reasoning_effort: 'high' as const, agent_loop: 'advanced' as const },
    };
    expect(resolveEffectiveTurnSettings(levels)).toEqual({
      reasoningEffort: { value: 'off', source: 'job' },
      agentLoop: { value: 'advanced', source: 'mode' },
    });
    expect(resolveEffectiveTurnSettings({ ...levels, job: null, mode: null })).toEqual({
      reasoningEffort: { value: 'low', source: 'thread' },
      agentLoop: { value: 'standard', source: 'thread' },
    });
    expect(resolveEffectiveTurnSettings({ ...levels, job: null, mode: null, thread: { agent_loop: null } })).toEqual({
      reasoningEffort: { value: 'medium', source: 'personality' },
      agentLoop: { value: 'standard', source: 'personality' },
    });
    expect(resolveEffectiveTurnSettings({ account: levels.account, personality: { reasoning_effort: null } })).toEqual({
      reasoningEffort: { value: 'high', source: 'account' },
      agentLoop: { value: 'advanced', source: 'account' },
    });
  });

  it('uses only the job and the thread in a sandboxed thread', () => {
    const inherited = {
      mode: { reasoning_effort: 'high' as const, agent_loop: 'advanced' as const },
      personality: { reasoning_effort: 'medium' as const, agent_loop: 'advanced' as const },
      account: { reasoning_effort: 'low' as const, agent_loop: 'advanced' as const },
      sandboxed: true,
    };
    expect(resolveEffectiveTurnSettings(inherited)).toEqual({
      reasoningEffort: { value: null, source: null },
      agentLoop: { value: null, source: null },
    });
    expect(resolveEffectiveTurnSettings({ ...inherited, thread: { reasoning_effort: 'off' } }).reasoningEffort).toEqual({
      value: 'off',
      source: 'thread',
    });
    expect(resolveEffectiveTurnSettings({ ...inherited, job: { agent_loop: 'standard' } }).agentLoop).toEqual({
      value: 'standard',
      source: 'job',
    });
  });

  it('falls back to not set when no level sets a value', () => {
    expect(resolveEffectiveTurnSettings({})).toEqual({
      reasoningEffort: { value: null, source: null },
      agentLoop: { value: null, source: null },
    });
  });

  it('skips invalid stored values', () => {
    const resolved = resolveEffectiveTurnSettings({
      thread: { reasoning_effort: 'medium' },
      mode: { reasoning_effort: 'bogus' as never, agent_loop: '' as never },
      account: { agent_loop: 'turbo' as never },
    });
    expect(resolved.reasoningEffort).toEqual({ value: 'medium', source: 'thread' });
    expect(resolved.agentLoop).toEqual({ value: null, source: null });
  });
});

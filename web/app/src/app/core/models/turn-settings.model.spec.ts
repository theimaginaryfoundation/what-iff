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
    const resolved = resolveEffectiveTurnSettings(
      { reasoning_effort: 'low', agent_loop: 'advanced' },
      { reasoning_effort: 'high', agent_loop: null },
    );
    expect(resolved.reasoningEffort).toEqual({ value: 'high', source: 'mode' });
    expect(resolved.agentLoop).toEqual({ value: 'advanced', source: 'thread' });
  });

  it('falls back to the thread, then to not set', () => {
    expect(resolveEffectiveTurnSettings({ reasoning_effort: 'off' }, null).reasoningEffort).toEqual({ value: 'off', source: 'thread' });
    expect(resolveEffectiveTurnSettings(null, undefined)).toEqual({
      reasoningEffort: { value: null, source: null },
      agentLoop: { value: null, source: null },
    });
  });

  it('skips invalid stored values', () => {
    const resolved = resolveEffectiveTurnSettings(
      { reasoning_effort: 'medium' },
      { reasoning_effort: 'bogus' as never, agent_loop: '' as never },
    );
    expect(resolved.reasoningEffort).toEqual({ value: 'medium', source: 'thread' });
    expect(resolved.agentLoop).toEqual({ value: null, source: null });
  });
});

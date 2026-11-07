import { mcpHostStyleVariables, toolDisplayName } from './mcp-app-host-styles';

describe('mcpHostStyleVariables', () => {
  it('resolves the spec names from the theme tokens', () => {
    const vars = mcpHostStyleVariables(name => ({ '--bg-card': ' #111 ', '--text-primary': '#eee', '--danger': '#f00' })[name] ?? '');
    expect(vars['--color-background-primary']).toBe('#111');
    expect(vars['--color-text-primary']).toBe('#eee');
    expect(vars['--color-background-danger']).toBe('color-mix(in srgb, #f00 12%, transparent)');
  });

  it('never hands a view an empty value when a token is missing', () => {
    const vars = mcpHostStyleVariables(() => '');
    expect(Object.values(vars).every(v => v.length > 0)).toBe(true);
  });
});

describe('toolDisplayName', () => {
  it.each([
    ['show_counter', 'Show counter'],
    ['get-weather', 'Get weather'],
    ['showCounter', 'Show Counter'],
    ['', 'App'],
  ])('%s reads %s', (raw, want) => {
    expect(toolDisplayName(raw)).toBe(want);
  });
});

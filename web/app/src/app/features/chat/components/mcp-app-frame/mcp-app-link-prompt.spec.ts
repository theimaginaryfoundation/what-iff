import { McpAppLinkPrompt } from './mcp-app-link-prompt';

describe('McpAppLinkPrompt', () => {
  it('opens the link the dialog showed and resolves true', async () => {
    const prompt = new McpAppLinkPrompt();
    const asked = prompt.ask('https://example.com/a?b=1');
    expect(prompt.pending()).toEqual({ url: 'https://example.com/a?b=1', host: 'example.com' });
    const open = vi.fn();
    prompt.answer(true, open);
    expect(open).toHaveBeenCalledExactlyOnceWith('https://example.com/a?b=1');
    await expect(asked).resolves.toBe(true);
    expect(prompt.pending()).toBeNull();
  });

  it('refuses a second link while one is on screen, so the view cannot swap it', async () => {
    const prompt = new McpAppLinkPrompt();
    const first = prompt.ask('https://shown.example/');
    await expect(prompt.ask('https://swapped.example/')).resolves.toBe(false);
    expect(prompt.pending()?.url).toBe('https://shown.example/');
    const open = vi.fn();
    prompt.answer(true, open);
    expect(open).toHaveBeenCalledExactlyOnceWith('https://shown.example/');
    await expect(first).resolves.toBe(true);
  });

  it('declines without opening, then takes the next request', async () => {
    const prompt = new McpAppLinkPrompt();
    const first = prompt.ask('https://a.example/');
    const open = vi.fn();
    prompt.answer(false, open);
    expect(open).not.toHaveBeenCalled();
    await expect(first).resolves.toBe(false);
    const second = prompt.ask('https://b.example/');
    expect(prompt.pending()?.host).toBe('b.example');
    prompt.cancel();
    await expect(second).resolves.toBe(false);
    prompt.answer(true, open); // nothing pending: no-op
    expect(open).not.toHaveBeenCalled();
  });
});

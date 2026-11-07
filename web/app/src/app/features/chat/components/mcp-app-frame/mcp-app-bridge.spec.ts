import { JsonRpcMessage, McpAppBridge, McpAppBridgeDeps, isWebUrl } from './mcp-app-bridge';

function setup(result: unknown = { content: [{ type: 'text', text: 'ok' }] }) {
  const posted: JsonRpcMessage[] = [];
  const deps: McpAppBridgeDeps = {
    callTool: vi.fn(async (name: string) => ({ content: [{ type: 'text', text: `ran ${name}` }] })),
    openLink: vi.fn(async () => true),
    proposeMessage: vi.fn(),
    resize: vi.fn(),
    post: m => posted.push(m),
  };
  const bridge = new McpAppBridge(deps, { theme: 'dark', toolName: 'show_chart', maxHeight: 800 }, { city: 'Oslo' }, result);
  return { bridge, deps, posted };
}

describe('McpAppBridge', () => {
  it('initializes, then sends the tool input and result once', async () => {
    const { bridge, posted } = setup();
    await bridge.handle({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: {} });
    expect(posted[0]).toMatchObject({ id: 1, result: { protocolVersion: '2026-01-26', hostContext: { theme: 'dark', displayMode: 'inline' } } });
    expect(posted).toHaveLength(1);

    await bridge.handle({ jsonrpc: '2.0', method: 'ui/notifications/initialized' });
    expect(posted[1]).toEqual({ jsonrpc: '2.0', method: 'ui/notifications/tool-input', params: { arguments: { city: 'Oslo' } } });
    expect(posted[2]).toMatchObject({ method: 'ui/notifications/tool-result', params: { content: [{ type: 'text', text: 'ok' }] } });

    await bridge.handle({ jsonrpc: '2.0', method: 'ui/notifications/initialized' });
    expect(posted).toHaveLength(3);
  });

  it('offers the style variables, reports ready, and passes on a theme change once started', async () => {
    const { bridge, deps, posted } = setup();
    const ready = vi.fn();
    deps.ready = ready;
    bridge.setTheme('light', { '--color-text-primary': '#111' });
    expect(posted).toHaveLength(0);

    await bridge.handle({ jsonrpc: '2.0', id: 1, method: 'ui/initialize', params: {} });
    expect(posted[0]).toMatchObject({ result: { hostContext: { theme: 'light', styles: { variables: { '--color-text-primary': '#111' } } } } });

    await bridge.handle({ jsonrpc: '2.0', method: 'ui/notifications/initialized' });
    expect(ready).toHaveBeenCalledTimes(1);

    posted.length = 0;
    bridge.setTheme('dark', { '--color-text-primary': '#eee' });
    expect(posted).toEqual([{
      jsonrpc: '2.0',
      method: 'ui/notifications/host-context-changed',
      params: { theme: 'dark', styles: { variables: { '--color-text-primary': '#eee' } } },
    }]);
  });

  it('proxies tools/call and reports failures as JSON-RPC errors', async () => {
    const { bridge, deps, posted } = setup();
    await bridge.handle({ jsonrpc: '2.0', id: 'a', method: 'tools/call', params: { name: 'refresh', arguments: { n: 1 } } });
    expect(deps.callTool).toHaveBeenCalledWith('refresh', { n: 1 });
    expect(posted[0]).toMatchObject({ id: 'a', result: { content: [{ text: 'ran refresh' }] } });

    (deps.callTool as ReturnType<typeof vi.fn>).mockRejectedValueOnce({ error: { error: 'this tool cannot be called from an app' } });
    await bridge.handle({ jsonrpc: '2.0', id: 'b', method: 'tools/call', params: { name: 'delete_all' } });
    expect(posted[1]).toEqual({ jsonrpc: '2.0', id: 'b', error: { code: -32000, message: 'this tool cannot be called from an app' } });
  });

  it('opens only web links, and only when the person agrees', async () => {
    const { bridge, deps, posted } = setup();
    await bridge.handle({ jsonrpc: '2.0', id: 1, method: 'ui/open-link', params: { url: 'javascript:alert(1)' } });
    expect(deps.openLink).not.toHaveBeenCalled();
    expect(posted[0].error?.code).toBe(-32602);
    (deps.openLink as ReturnType<typeof vi.fn>).mockResolvedValueOnce(false);
    await bridge.handle({ jsonrpc: '2.0', id: 2, method: 'ui/open-link', params: { url: 'https://example.com' } });
    expect(posted[1].error?.message).toContain('chose not to open');
  });

  it('puts ui/message text in the composer, resizes, and refuses unknown methods', async () => {
    const { bridge, deps, posted } = setup();
    await bridge.handle({ jsonrpc: '2.0', id: 1, method: 'ui/message', params: { role: 'user', content: { type: 'text', text: 'Book it' } } });
    expect(deps.proposeMessage).toHaveBeenCalledWith('Book it');
    await bridge.handle({ jsonrpc: '2.0', method: 'ui/notifications/size-changed', params: { width: 300, height: 420 } });
    expect(deps.resize).toHaveBeenCalledWith(420);
    await bridge.handle({ jsonrpc: '2.0', id: 9, method: 'resources/read', params: {} });
    expect(posted.at(-1)?.error?.code).toBe(-32601);
    await bridge.handle({ not: 'json-rpc' });
    await bridge.handle({ jsonrpc: '2.0', id: 3, result: {} });
    expect(posted).toHaveLength(2);
  });

  it('only asks a started view to tear down', () => {
    const { bridge, posted } = setup();
    bridge.teardown('closed');
    expect(posted).toHaveLength(0);
  });

  it('recognises web urls', () => {
    expect(isWebUrl('https://a.example')).toBe(true);
    expect(isWebUrl('data:text/html,hi')).toBe(false);
  });
});

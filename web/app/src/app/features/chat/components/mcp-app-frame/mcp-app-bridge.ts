/**
 * The host side of the MCP Apps postMessage protocol (SEP-1865, spec 2026-01-26; ADR 0x042): JSON-RPC
 * between a view in a sandboxed frame and the chat. Kept free of Angular and the DOM so it can be
 * tested on its own; McpAppFrameComponent wires it to the frame.
 *
 * Supported: ui/initialize, ui/notifications/initialized (then tool-input and tool-result are
 * sent), tools/call (proxied to the view's own connector), ui/open-link (after the person
 * confirms), ui/message (put in the composer for the person to send), size-changed, logging and
 * ping. ui/update-model-context is accepted and ignored for now; display modes other than inline
 * and resources/read are not offered.
 */

export const MCP_APPS_PROTOCOL_VERSION = '2026-01-26';

export interface JsonRpcMessage {
  jsonrpc: '2.0';
  id?: string | number | null;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { code: number; message: string };
}

export interface McpAppBridgeDeps {
  /** Runs a tool on the view's own connector; resolves to the raw CallToolResult. */
  callTool(name: string, args: unknown): Promise<unknown>;
  /** Opens an http(s) link after the person agrees; resolves to whether it was opened. */
  openLink(url: string): Promise<boolean>;
  /** Proposes text as the user's next message. */
  proposeMessage(text: string): void;
  /** The view's content height changed. */
  resize(height: number): void;
  /** Sends a message into the frame. */
  post(message: JsonRpcMessage): void;
  log?(level: string, data: unknown): void;
  /** The view finished its handshake and has its data, so it is safe to show. */
  ready?(): void;
}

export interface McpAppHostContext {
  theme: 'light' | 'dark';
  toolName: string;
  maxHeight: number;
  locale?: string;
  timeZone?: string;
  /** The chat's theme in the spec's style variables. */
  styleVariables?: Record<string, string>;
}

export class McpAppBridge {
  private initialized = false;
  private toolSent = false;

  constructor(
    private readonly deps: McpAppBridgeDeps,
    private context: McpAppHostContext,
    private readonly toolInput: unknown,
    private toolResult: unknown,
  ) {}

  /** The result arrived after the view started (it is sent once the view is initialized). */
  setToolResult(result: unknown): void {
    this.toolResult = result;
    if (this.initialized && this.toolSent && result !== undefined) {
      this.notify('ui/notifications/tool-result', result);
    }
  }

  /** The chat's theme changed; a view that has started hears about it, one that hasn't gets it on initialize. */
  setTheme(theme: 'light' | 'dark', styleVariables: Record<string, string>): void {
    this.context = { ...this.context, theme, styleVariables };
    if (!this.initialized) return;
    this.notify('ui/notifications/host-context-changed', { theme, styles: { variables: styleVariables } });
  }

  /** Asks the view to clean up before the frame goes away. */
  teardown(reason: string): void {
    if (!this.initialized) return;
    this.deps.post({ jsonrpc: '2.0', id: `teardown-${Date.now()}`, method: 'ui/resource-teardown', params: { reason } });
  }

  /** Handles one message from the view. */
  async handle(raw: unknown): Promise<void> {
    if (!isJsonRpc(raw) || !raw.method) return; // responses to our requests need nothing
    const isRequest = raw.id !== undefined && raw.id !== null;
    try {
      const result = await this.dispatch(raw.method, raw.params);
      if (isRequest) this.deps.post({ jsonrpc: '2.0', id: raw.id, result: result ?? {} });
    } catch (err) {
      if (!isRequest) return;
      const error = err instanceof RpcFailure ? err : new RpcFailure(-32000, errorText(err));
      this.deps.post({ jsonrpc: '2.0', id: raw.id, error: { code: error.code, message: error.message } });
    }
  }

  private async dispatch(method: string, params: unknown): Promise<unknown> {
    const p = (params ?? {}) as Record<string, unknown>;
    switch (method) {
      case 'ui/initialize':
        return this.initializeResult();
      case 'ui/notifications/initialized':
        this.initialized = true;
        this.sendTool();
        this.deps.ready?.();
        return undefined;
      case 'tools/call': {
        const name = typeof p['name'] === 'string' ? p['name'] : '';
        if (!name) throw new RpcFailure(-32602, 'tools/call needs a tool name');
        return this.deps.callTool(name, p['arguments'] ?? {});
      }
      case 'ui/open-link': {
        const url = typeof p['url'] === 'string' ? p['url'] : '';
        if (!isWebUrl(url)) throw new RpcFailure(-32602, 'Only http and https links can be opened');
        const opened = await this.deps.openLink(url);
        if (!opened) throw new RpcFailure(-32000, 'The user chose not to open the link');
        return {};
      }
      case 'ui/message': {
        const text = messageText(p['content']);
        if (!text) throw new RpcFailure(-32602, 'ui/message needs text content');
        this.deps.proposeMessage(text);
        return {};
      }
      case 'ui/update-model-context':
        return {};
      case 'ui/request-display-mode':
        return { mode: 'inline' };
      case 'ui/notifications/size-changed': {
        const height = Number(p['height']);
        if (Number.isFinite(height) && height > 0) this.deps.resize(height);
        return undefined;
      }
      case 'notifications/message':
        this.deps.log?.(String(p['level'] ?? 'info'), p['data']);
        return undefined;
      case 'ping':
        return {};
      default:
        throw new RpcFailure(-32601, `Method not supported: ${method}`);
    }
  }

  private initializeResult(): unknown {
    const c = this.context;
    return {
      protocolVersion: MCP_APPS_PROTOCOL_VERSION,
      hostInfo: { name: 'WhatIff', version: '1.0.0' },
      hostCapabilities: { openLinks: {}, serverTools: {}, logging: {} },
      hostContext: {
        theme: c.theme,
        ...(c.styleVariables ? { styles: { variables: c.styleVariables } } : {}),
        displayMode: 'inline',
        availableDisplayModes: ['inline'],
        containerDimensions: { maxHeight: c.maxHeight },
        locale: c.locale,
        timeZone: c.timeZone,
        platform: 'web',
        toolInfo: { tool: { name: c.toolName } },
      },
    };
  }

  /** Sends the tool's input, then its result (when known), once per view. */
  private sendTool(): void {
    if (this.toolSent) return;
    this.toolSent = true;
    this.notify('ui/notifications/tool-input', { arguments: this.toolInput ?? {} });
    if (this.toolResult !== undefined && this.toolResult !== null) {
      this.notify('ui/notifications/tool-result', this.toolResult);
    }
  }

  private notify(method: string, params: unknown): void {
    this.deps.post({ jsonrpc: '2.0', method, params });
  }
}

class RpcFailure extends Error {
  constructor(readonly code: number, message: string) {
    super(message);
  }
}

function isJsonRpc(raw: unknown): raw is JsonRpcMessage {
  return !!raw && typeof raw === 'object' && (raw as JsonRpcMessage).jsonrpc === '2.0';
}

export function isWebUrl(url: string): boolean {
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' || parsed.protocol === 'http:';
  } catch {
    return false;
  }
}

/** The text of a ui/message content: one text block, or a list of blocks. */
function messageText(content: unknown): string {
  const blocks = Array.isArray(content) ? content : [content];
  return blocks
    .map(b => (b && typeof b === 'object' && (b as { type?: unknown }).type === 'text' ? String((b as { text?: unknown }).text ?? '') : ''))
    .filter(Boolean)
    .join('\n')
    .trim();
}

function errorText(err: unknown): string {
  if (err && typeof err === 'object') {
    const e = err as { error?: { error?: string; message?: string }; message?: string };
    return e.error?.error || e.error?.message || e.message || 'The tool call failed';
  }
  return 'The tool call failed';
}

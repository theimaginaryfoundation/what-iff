import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnChanges, SimpleChanges, inject, input, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { Chat } from '../../../../../core/models/chat.model';
import { MCPServer } from '../../../../../core/models/mcp-server.model';
import { ToolCall } from '../../../../../core/models/toolcall.model';
import { MCPServerService } from '../../../../../core/services/mcp-server.service';
import { ToolMeta, ToolService } from '../../../../../core/services/tool.service';
import { ChatService } from '../../../../../core/services/chat.service';
import { apiErrorMessage } from '../../../../../core/utils/api-error.helpers';
import { ContextPanelService } from '../../../services/context-panel.service';

type ToolContextTab = 'available' | 'history';

@Component({
  selector: 'app-context-tools-tab',
  standalone: true,
  imports: [CommonModule],
  template: `
    <section class="tab-body">
      <div class="context-tabs" role="tablist" aria-label="Tools view">
        <button
          type="button"
          role="tab"
          [attr.aria-selected]="activeToolTab() === 'available'"
          [class.context-tabs__button--active]="activeToolTab() === 'available'"
          (click)="activeToolTab.set('available')"
        >
          Available Tools
        </button>
        <button
          type="button"
          role="tab"
          [attr.aria-selected]="activeToolTab() === 'history'"
          [class.context-tabs__button--active]="activeToolTab() === 'history'"
          (click)="activeToolTab.set('history')"
        >
          Tool Call History
        </button>
      </div>

      @if (activeToolTab() === 'available') {
        @if (toolsLoading()) {
          <p class="state">Loading tools…</p>
        } @else {
          <ul class="list">
            @for (tool of tools(); track tool.name) {
              <li>
                <label class="tool-item">
                  <input
                    type="checkbox"
                    [checked]="isToolEnabled(tool.name)"
                    (change)="toggleTool(tool.name, $any($event.target).checked)"
                  />
                  <span>{{ tool.name }}</span>
                  <small>{{ tool.description }}</small>
                </label>
              </li>
            }
          </ul>
        }
      } @else {
        @if (toolCalls().length === 0) {
          <p class="state">No tool calls loaded for this thread.</p>
        } @else {
          <ul class="history-list">
            @for (toolCall of toolCalls(); track toolCall.id) {
              <li class="history-item">
                <time [attr.datetime]="toolCall.created_at">{{ toolCall.created_at | date: 'MMM d h:mm a' }}</time>
                <strong>{{ friendlyToolName(toolCall.tool_name) }}</strong>
                <span>{{ toolInputSummary(toolCall) }}</span>
              </li>
            }
          </ul>
        }
      }

      <details class="mcp-section">
        <summary>MCP Servers</summary>
      @if (mcpLoading()) {
        <p class="state">Loading MCP servers…</p>
      } @else {
        <div class="mcp-grid">
          <div>
            <h4>Connected</h4>
            <ul>
              @for (server of connected(); track server.id) {
                <li>
                  {{ server.name }}
                  <button type="button" (click)="detach(server)">Detach</button>
                </li>
              }
            </ul>
          </div>
          <div>
            <h4>Available</h4>
            <ul>
              @for (server of available(); track server.id) {
                <li>
                  {{ server.name }}
                  <button type="button" (click)="attach(server)">Attach</button>
                </li>
              }
            </ul>
          </div>
        </div>
      }
      </details>
      @if (error(); as error) {
        <p class="error" role="alert">{{ error }}</p>
      }
      <button type="button" class="link" (click)="openIntegrations()">Manage integrations</button>
    </section>
  `,
  styles: [`
    .tab-body {
      display: grid;
      gap: 0.75rem;
    }

    h3 {
      margin: 0;
      font-size: 0.75rem;
      text-transform: uppercase;
      letter-spacing: 0.06em;
      color: var(--color-text-muted);
    }

    .context-tabs {
      border-radius: 0.375rem;
      display: flex;
      overflow: hidden;
    }

    .context-tabs button {
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      flex: 1;
      font-size: 0.625rem;
      font-weight: 700;
      padding: 0.3125rem 0;
    }

    .context-tabs .context-tabs__button--active {
      background: color-mix(in srgb, var(--color-accent) 14%, transparent);
      color: var(--color-accent);
    }

    h4 {
      margin: 0 0 0.35rem;
      font-size: 0.8rem;
      color: var(--color-text-muted);
      text-transform: uppercase;
    }

    .list,
    .mcp-grid ul {
      list-style: none;
      margin: 0;
      padding: 0;
      display: grid;
      gap: 0.45rem;
    }

    .tool-item {
      border-bottom: 1px solid var(--color-border-base);
      display: grid;
      gap: 0.15rem;
      grid-template-columns: auto minmax(0, 1fr);
      padding: 0 0 0.5rem;
    }

    .tool-item input {
      grid-row: span 2;
      margin-top: 0.125rem;
    }

    .tool-item span {
      font-size: 0.8125rem;
      font-weight: 600;
    }

    .tool-item small {
      color: var(--color-text-muted);
      font-size: 0.75rem;
    }

    .history-list {
      display: grid;
      gap: 0.625rem;
      list-style: none;
      margin: 0;
      padding: 0;
    }

    .history-item {
      border-bottom: 1px solid var(--color-border-base);
      display: grid;
      gap: 0.125rem;
      padding-bottom: 0.5rem;
    }

    .history-item time {
      color: var(--color-text-muted);
      font-size: 0.625rem;
      text-transform: uppercase;
    }

    .history-item strong {
      font-size: 0.8125rem;
    }

    .history-item span {
      color: var(--color-text-secondary);
      font-size: 0.75rem;
      overflow-wrap: anywhere;
    }

    .mcp-section {
      color: var(--color-text-secondary);
      font-size: 0.8125rem;
    }

    .mcp-grid {
      display: grid;
      gap: 0.75rem;
      grid-template-columns: minmax(0, 1fr);
    }

    .mcp-grid li {
      align-items: center;
      background: var(--color-surface-base);
      border: 1px solid var(--color-border-base);
      border-radius: 0.5rem;
      display: flex;
      justify-content: space-between;
      gap: 0.5rem;
      min-width: 0;
      padding: 0.45rem 0.5rem;
    }

    .mcp-grid li {
      overflow-wrap: anywhere;
    }

    .mcp-grid button {
      border: 1px solid var(--color-border-base);
      border-radius: 0.45rem;
      font-size: 0.72rem;
      padding: 0.2rem 0.45rem;
    }

    .state {
      color: var(--color-text-muted);
      margin: 0;
      font-size: 0.85rem;
    }

    .error {
      color: var(--color-danger);
      margin: 0;
      font-size: 0.85rem;
    }

    .link {
      background: transparent;
      border: 0;
      color: var(--color-accent);
      font-size: 0.8rem;
      justify-self: start;
      padding: 0;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextToolsTabComponent implements OnChanges {
  readonly chat = input<Chat | null>(null);
  readonly toolCalls = input<readonly ToolCall[]>([]);

  private readonly toolService = inject(ToolService);
  private readonly mcpService = inject(MCPServerService);
  private readonly chatService = inject(ChatService);
  private readonly contextPanel = inject(ContextPanelService);
  private readonly router = inject(Router);

  readonly toolsLoading = signal(false);
  readonly mcpLoading = signal(false);
  readonly error = signal<string | null>(null);
  readonly activeToolTab = signal<ToolContextTab>('available');
  readonly tools = signal<ToolMeta[]>([]);
  readonly connected = signal<MCPServer[]>([]);
  readonly available = signal<MCPServer[]>([]);

  ngOnChanges(changes: SimpleChanges): void {
    const chatChange = changes['chat'];
    if (!chatChange) {
      return;
    }
    const prevId = chatChange.previousValue?.id;
    const nextId = chatChange.currentValue?.id;
    if (prevId !== nextId) {
      void this.loadForChat();
    }
  }

  isToolEnabled(toolName: string): boolean {
    const chat = this.chat();
    const disabled = chat?.disabled_tools ?? [];
    return !disabled.includes(toolName);
  }

  async toggleTool(toolName: string, enabled: boolean): Promise<void> {
    const chat = this.chat();
    if (!chat) return;
    const disabled = new Set(chat.disabled_tools ?? []);
    if (enabled) {
      disabled.delete(toolName);
    } else {
      disabled.add(toolName);
    }

    try {
      const updated = await firstValueFrom(
        this.chatService.patchChat(chat.id, { disabled_tools: [...disabled] }),
      );
      this.contextPanel.setActiveChat(updated);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to update tool settings'));
    }
  }

  async attach(server: MCPServer): Promise<void> {
    const chatId = this.chat()?.id;
    if (!chatId) return;
    try {
      await firstValueFrom(this.mcpService.addToChat(chatId, server.id));
      await this.loadMcp(chatId);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to attach MCP server'));
    }
  }

  async detach(server: MCPServer): Promise<void> {
    const chatId = this.chat()?.id;
    if (!chatId) return;
    try {
      await firstValueFrom(this.mcpService.removeFromChat(chatId, server.id));
      await this.loadMcp(chatId);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to detach MCP server'));
    }
  }

  openIntegrations(): void {
    void this.router.navigate(['/integrations']);
  }

  friendlyToolName(name: string): string {
    return name
      .split(/[_-]/)
      .filter(Boolean)
      .map(part => part.charAt(0).toUpperCase() + part.slice(1))
      .join(' ');
  }

  toolInputSummary(toolCall: ToolCall): string {
    const input = toolCall.tool_input?.trim();
    if (!input) return 'No input recorded.';
    try {
      const parsed = JSON.parse(input) as Record<string, unknown>;
      const query = parsed['query'] ?? parsed['search'] ?? parsed['url'] ?? parsed['path'];
      if (typeof query === 'string' && query.trim()) {
        const label = toolCall.tool_name.toLowerCase().includes('search') ? 'Search' : 'Input';
        return `${label}: "${query.trim()}"`;
      }
    } catch {
      // Fall back to compact raw text below.
    }
    return input.length > 96 ? `${input.slice(0, 93)}...` : input;
  }

  private async loadForChat(): Promise<void> {
    const chatId = this.chat()?.id;
    this.error.set(null);
    await Promise.all([this.loadTools(), this.loadMcp(chatId)]);
  }

  private async loadTools(): Promise<void> {
    this.toolsLoading.set(true);
    try {
      this.tools.set(await firstValueFrom(this.toolService.listTools()));
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to load tools'));
    } finally {
      this.toolsLoading.set(false);
    }
  }

  private async loadMcp(chatId: string | undefined): Promise<void> {
    if (!chatId) {
      this.connected.set([]);
      this.available.set([]);
      return;
    }
    this.mcpLoading.set(true);
    try {
      const [active, availablePage] = await Promise.all([
        firstValueFrom(this.mcpService.listActiveForChat(chatId)),
        firstValueFrom(this.mcpService.listAvailableForChat(chatId, 1, 20)),
      ]);
      this.connected.set(active ?? []);
      this.available.set(availablePage.results ?? []);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to load MCP servers'));
    } finally {
      this.mcpLoading.set(false);
    }
  }
}

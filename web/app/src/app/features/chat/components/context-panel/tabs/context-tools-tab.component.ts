import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, OnChanges, SimpleChanges, computed, inject, input, signal } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { Chat } from '../../../../../core/models/chat.model';
import { MCPServer } from '../../../../../core/models/mcp-server.model';
import { ToolCall } from '../../../../../core/models/toolcall.model';
import { MCPServerService } from '../../../../../core/services/mcp-server.service';
import { ToolMeta, ToolService } from '../../../../../core/services/tool.service';
import { RitualService } from '../../../../../core/services/ritual.service';
import { LoadedSkill } from '../../../../../core/models/ritual.model';
import { ChatService } from '../../../../../core/services/chat.service';
import { apiErrorMessage } from '../../../../../core/utils/api-error.helpers';
import { ContextPanelService } from '../../../services/context-panel.service';
import { friendlyToolName, summarizeToolInput } from '../../../helpers/tool-call-format.helpers';
import { HelpHintComponent } from '../../../../../shared/ui/help-hint/help-hint.component';
import { TooltipDirective } from '../../../../../shared/ui/tooltip/tooltip.directive';
import { CheckboxComponent } from '../../../../../shared/ui/checkbox/checkbox.component';
import { matchesQuery } from '../../../helpers/panel-search.helpers';
import { ImageModelPickerComponent } from './image-model-picker.component';

type ToolContextTab = 'available' | 'history';

@Component({
  selector: 'app-context-tools-tab',
  standalone: true,
  imports: [CheckboxComponent, CommonModule, HelpHintComponent, ImageModelPickerComponent, TooltipDirective],
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
          Available tools
        </button>
        <button
          type="button"
          role="tab"
          [attr.aria-selected]="activeToolTab() === 'history'"
          [class.context-tabs__button--active]="activeToolTab() === 'history'"
          (click)="activeToolTab.set('history')"
        >
          Tool call history
        </button>
      </div>

      @if (activeToolTab() === 'available') {
        <div class="finder" role="search">
          <label class="visually-hidden" for="context-panel-search">Search tools, skills and MCP servers</label>
          <input
            id="context-panel-search"
            type="search"
            class="finder__input"
            data-testid="context-panel-search"
            placeholder="Search tools, skills, MCP servers"
            autocomplete="off"
            [value]="searchText()"
            (input)="searchText.set($any($event.target).value)"
            (keydown.escape)="clearSearch($event)"
          />
          <p class="finder__count" role="status">{{ resultsSummary() }}</p>
        </div>
      }

      @if (activeToolTab() === 'available') {
        @if (toolsLoading()) {
          <p class="state">Loading tools…</p>
        } @else {
          <div class="label-row">
            <span class="label">Tools for this thread</span>
            <ui-help-hint label="What are tools?" heading="Tools" align="end">
              Actions the personality can take while replying, such as searching the web or saving a memory. Unticking a tool turns it off
              for this thread only. A sandboxed thread starts with most tools off.
            </ui-help-hint>
          </div>
          <ul class="list">
            @for (tool of visibleTools(); track tool.name) {
              <li [class.list__item--with-extra]="tool.name === 'generate_image'">
                <label class="tool-item" [uiTooltip]="tool.guide ?? ''" placement="left">
                  <ui-checkbox
                    [checked]="isToolEnabled(tool.name)"
                    (checkedChange)="toggleTool(tool.name, $event)"
                  />
                  <span>{{ friendlyToolName(tool.name) }}</span>
                  <small>{{ tool.description }}</small>
                </label>
                @if (tool.name === 'generate_image') {
                  <app-image-model-picker />
                }
              </li>
            }
          </ul>
          @if (query() && totalMatches() === 0) {
            <p class="state">Nothing matches "{{ query() }}".</p>
          }
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

      @if (!query() || visibleSkills().length > 0) {
      <details class="mcp-section" open>
        <summary>
          Loaded skills
          <span class="summary-hint" (click)="$event.preventDefault()">
            <ui-help-hint label="What are loaded skills?" heading="Loaded skills" align="end">
              Skills the personality loaded itself for this thread. Their instructions stay in context until unloaded here,
              by the personality, or when a checkpoint finds them unused. Skills called with / or set on a mode apply to
              their turn and are not listed.
            </ui-help-hint>
          </span>
        </summary>
        @if (skillsLoading()) {
          <p class="state">Loading skills…</p>
        } @else if (loadedSkills().length === 0) {
          <p class="state">No skills loaded in this thread.</p>
        } @else {
          <div class="mcp-grid">
            <ul>
              @for (skill of visibleSkills(); track skill.id) {
                <li>
                  <span class="skill-name" [uiTooltip]="skill.description">{{ skill.name }} <code>{{ skill.slug }}</code></span>
                  <button type="button" uiTooltip="Drop this skill's instructions from the thread" (click)="unloadSkill(skill)">Unload</button>
                </li>
              }
            </ul>
          </div>
        }
      </details>
      }

      @if (!query() || visibleConnected().length + visibleAvailable().length > 0) {
      <details class="mcp-section" [open]="!!query()">
        <summary>
          MCP servers
          <!-- preventDefault keeps a click on the hint from also toggling the details. -->
          <span class="summary-hint" (click)="$event.preventDefault()">
            <ui-help-hint label="What are MCP servers?" heading="MCP servers" align="end">
              External services that give the personality extra tools. Attach one to use its tools in this thread; set servers up under
              Manage integrations.
            </ui-help-hint>
          </span>
        </summary>
        @if (mcpLoading()) {
          <p class="state">Loading MCP servers…</p>
        } @else {
          <div class="mcp-grid">
            <div>
              <h4>Connected</h4>
              <ul>
                @for (server of visibleConnected(); track server.id) {
                  <li>
                    {{ server.name }}
                    <button type="button" uiTooltip="Stop using this server's tools in this thread" (click)="detach(server)">Detach</button>
                  </li>
                }
              </ul>
            </div>
            <div>
              <h4>Available</h4>
              <ul>
                @for (server of visibleAvailable(); track server.id) {
                  <li>
                    {{ server.name }}
                    <button type="button" uiTooltip="Use this server's tools in this thread" (click)="attach(server)">Attach</button>
                  </li>
                }
              </ul>
            </div>
          </div>
        }
      </details>
      }
      @if (error(); as error) {
        <p class="error" role="alert">{{ error }}</p>
      }
      <button type="button" class="link" (click)="openIntegrations()">Manage integrations</button>
    </section>
  `,
  styles: [
    `
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

      /* A tool with extra controls under it: the divider goes below them, not between. */
      .list__item--with-extra {
        border-bottom: 1px solid var(--color-border-base);
        padding-bottom: 0.5rem;
      }

      .list__item--with-extra .tool-item {
        border-bottom: 0;
        padding-bottom: 0;
      }

      .tool-item ui-checkbox {
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

      .visually-hidden {
        border: 0;
        clip: rect(0 0 0 0);
        height: 1px;
        margin: -1px;
        overflow: hidden;
        padding: 0;
        position: absolute;
        white-space: nowrap;
        width: 1px;
      }

      /* 9.18:1 on the input background (WCAG AAA needs 7:1); the theme's own placeholder gray is 2.35:1. */
      .finder__input::placeholder {
        color: #b4bbc6;
        opacity: 1;
      }

      .finder__input:focus-visible {
        outline: 2px solid var(--color-accent);
        outline-offset: 2px;
      }

      .finder__input {
        min-height: 2.25rem;
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 0.75rem;
        color: var(--color-text-primary);
        font-size: 0.8125rem;
        padding: 0.5rem 0.75rem;
        width: 100%;
      }

      .finder__count {
        color: var(--color-text-muted);
        font-size: 0.75rem;
        margin: 0.25rem 0 0;
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

      .label-row {
        align-items: center;
        display: flex;
        gap: 0.5rem;
        justify-content: space-between;
      }

      .label {
        color: var(--color-text-muted);
        font-size: 0.75rem;
        font-weight: 700;
        text-transform: uppercase;
      }

      .summary-hint {
        display: inline-flex;
        vertical-align: middle;
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

      .skill-name code {
        color: var(--color-text-muted);
        font-size: 0.7rem;
        margin-left: 0.25rem;
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
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextToolsTabComponent implements OnChanges {
  readonly chat = input<Chat | null>(null);
  readonly toolCalls = input<readonly ToolCall[]>([]);

  private readonly toolService = inject(ToolService);
  private readonly mcpService = inject(MCPServerService);
  private readonly ritualService = inject(RitualService);
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
  readonly skillsLoading = signal(false);
  readonly loadedSkills = signal<LoadedSkill[]>([]);

  /** Raw text of the panel's search box. One box filters tools, loaded skills and MCP servers. */
  readonly searchText = signal('');
  /** The trimmed query, in effect only on the Available view (history is a different data set). */
  readonly query = computed(() => (this.activeToolTab() === 'available' ? this.searchText().trim() : ''));
  readonly visibleTools = computed(() =>
    this.tools().filter(tool => matchesQuery(this.query(), friendlyToolName(tool.name), tool.description)),
  );
  readonly visibleSkills = computed(() =>
    this.loadedSkills().filter(skill => matchesQuery(this.query(), skill.name, skill.slug, skill.description)),
  );
  readonly visibleConnected = computed(() => this.connected().filter(s => matchesQuery(this.query(), s.name, s.description)));
  readonly visibleAvailable = computed(() => this.available().filter(s => matchesQuery(this.query(), s.name, s.description)));
  readonly totalMatches = computed(
    () => this.visibleTools().length + this.visibleSkills().length + this.visibleConnected().length + this.visibleAvailable().length,
  );
  /** Announced through the status region; empty while nothing is being searched. */
  readonly resultsSummary = computed(() => {
    if (!this.query()) return '';
    const total = this.totalMatches();
    if (total === 0) return `No results for ${this.query()}`;
    const servers = this.visibleConnected().length + this.visibleAvailable().length;
    const count = (n: number, one: string, many: string): string => `${n} ${n === 1 ? one : many}`;
    return `${count(total, 'result', 'results')}: ${count(this.visibleTools().length, 'tool', 'tools')}, ${count(this.visibleSkills().length, 'skill', 'skills')}, ${count(servers, 'MCP server', 'MCP servers')}`;
  });

  clearSearch(event: Event): void {
    if (!this.searchText()) return;
    event.preventDefault();
    this.searchText.set('');
  }

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
      const updated = await firstValueFrom(this.chatService.patchChat(chat.id, { disabled_tools: [...disabled] }));
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

  async unloadSkill(skill: LoadedSkill): Promise<void> {
    const chatId = this.chat()?.id;
    if (!chatId) return;
    try {
      await firstValueFrom(this.ritualService.unloadSkill(chatId, skill.id));
      await this.loadSkills(chatId);
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to unload skill'));
    }
  }

  openIntegrations(): void {
    void this.router.navigate(['/integrations']);
  }

  readonly friendlyToolName = friendlyToolName;

  toolInputSummary(toolCall: ToolCall): string {
    const summary = summarizeToolInput(toolCall.tool_input);
    if (!summary) return 'No input recorded.';
    const label = toolCall.tool_name.toLowerCase().includes('search') ? 'Search' : 'Input';
    return `${label}: ${summary}`;
  }

  private async loadForChat(): Promise<void> {
    const chatId = this.chat()?.id;
    this.error.set(null);
    await Promise.all([this.loadTools(), this.loadMcp(chatId), this.loadSkills(chatId)]);
  }

  private async loadSkills(chatId: string | undefined): Promise<void> {
    if (!chatId) {
      this.loadedSkills.set([]);
      return;
    }
    this.skillsLoading.set(true);
    try {
      this.loadedSkills.set(await firstValueFrom(this.ritualService.listLoadedSkills(chatId)));
    } catch (error) {
      this.error.set(apiErrorMessage(error, 'Failed to load the thread\'s skills'));
    } finally {
      this.skillsLoading.set(false);
    }
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

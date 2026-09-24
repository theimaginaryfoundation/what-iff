import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { ContextPanelService } from '../../services/context-panel.service';
import { ContextBreakdownTabComponent } from './tabs/context-breakdown-tab.component';
import { ContextMemoriesTabComponent } from './tabs/context-memories-tab.component';
import { ContextScratchpadTabComponent } from './tabs/context-scratchpad-tab.component';
import { ContextToolsTabComponent } from './tabs/context-tools-tab.component';
import {
  BrainIconComponent,
  LayersIconComponent,
  NoteIconComponent,
  WrenchIconComponent,
  XIconComponent,
} from '../../../../shared/ui/icons/icons';

@Component({
  selector: 'app-context-panel',
  standalone: true,
  imports: [
    ContextScratchpadTabComponent,
    ContextMemoriesTabComponent,
    ContextToolsTabComponent,
    ContextBreakdownTabComponent,
    XIconComponent,
    NoteIconComponent,
    BrainIconComponent,
    WrenchIconComponent,
    LayersIconComponent,
  ],
  template: `
    <section class="context-panel" aria-label="Conversation context">
      <header class="context-panel__header">
        <span id="conversation-context-panel-title" class="context-panel__header-title">{{ activeTabLabel() }}</span>
        <button
          type="button"
          class="context-panel__close"
          aria-label="Close context panel"
          (click)="closePanel()"
        >
          <ui-x-icon [size]="14" />
        </button>
      </header>

      <nav class="context-panel__primary-tabs" aria-label="Context sections">
        <button
          type="button"
          [class.context-panel__primary-tab--active]="context.activeTab() === 'scratchpad'"
          [attr.aria-current]="context.activeTab() === 'scratchpad' ? 'page' : null"
          title="Scratchpad"
          (click)="context.setActiveTab('scratchpad')"
        >
          <ui-note-icon class="context-panel__primary-tab-icon" [size]="20" />
          <span class="context-panel__primary-tab-label">Scratchpad</span>
        </button>
        <button
          type="button"
          [class.context-panel__primary-tab--active]="context.activeTab() === 'memories'"
          [attr.aria-current]="context.activeTab() === 'memories' ? 'page' : null"
          title="Memories"
          (click)="context.setActiveTab('memories')"
        >
          <ui-brain-icon class="context-panel__primary-tab-icon" [size]="20" />
          <span class="context-panel__primary-tab-label">Memories</span>
        </button>
        <button
          type="button"
          [class.context-panel__primary-tab--active]="context.activeTab() === 'tools'"
          [attr.aria-current]="context.activeTab() === 'tools' ? 'page' : null"
          title="Tools"
          (click)="context.setActiveTab('tools')"
        >
          <ui-wrench-icon class="context-panel__primary-tab-icon" [size]="20" />
          <span class="context-panel__primary-tab-label">Tools</span>
        </button>
        <button
          type="button"
          [class.context-panel__primary-tab--active]="context.activeTab() === 'context'"
          [attr.aria-current]="context.activeTab() === 'context' ? 'page' : null"
          title="Context"
          (click)="context.setActiveTab('context')"
        >
          <ui-layers-icon class="context-panel__primary-tab-icon" [size]="20" />
          <span class="context-panel__primary-tab-label">Context</span>
        </button>
      </nav>

      <div class="context-panel__body">
        @switch (context.activeTab()) {
          @case ('scratchpad') {
          <app-context-scratchpad-tab
            [chatId]="context.activeChatId()"
            [canSave]="!!context.activeChat()?.personality_id"
            [personalityName]="context.activeChat()?.personality_name ?? null"
          />
          }
          @case ('memories') {
          <app-context-memories-tab
            [chatId]="context.activeChatId()"
            [personalityId]="context.activeChat()?.personality_id ?? null"
          />
          }
          @case ('tools') {
          <app-context-tools-tab [chat]="context.activeChat()" [toolCalls]="context.toolCalls()" />
          }
          @case ('context') {
          <app-context-breakdown-tab
            [breakdown]="context.shownBreakdown()"
            [messageId]="context.shownBreakdownId()"
          />
          }
        }
      </div>
    </section>
  `,
  styles: [`
    .context-panel {
      display: grid;
      grid-template-rows: auto minmax(0, 1fr);
      height: 100%;
      min-height: 0;
    }

    .context-panel__header {
      align-items: center;
      border-bottom: 1px solid var(--color-border-base);
      color: var(--color-text-muted);
      display: flex;
      flex-shrink: 0;
      font-size: 0.625rem;
      font-weight: 700;
      justify-content: space-between;
      letter-spacing: 0.06em;
      padding: 0.625rem 0.875rem 0.5rem;
      text-transform: uppercase;
    }

    .context-panel__close {
      align-items: center;
      background: transparent;
      border: 0;
      color: var(--color-text-muted);
      cursor: pointer;
      display: inline-flex;
      flex-shrink: 0;
      justify-content: center;
      padding: 0.25rem;
      transition: color 120ms ease;
    }

    .context-panel__close:hover,
    .context-panel__close:focus-visible {
      color: var(--color-accent);
    }

    .context-panel__body {
      display: flex;
      flex-direction: column;
      min-height: 0;
      overflow-y: auto;
      padding: 0.875rem;
    }

    .context-panel__primary-tabs {
      display: none;
    }

    @media (max-width: 1023px) {
      /* Title, tab strip, then a body that takes the remaining height. */
      .context-panel {
        grid-template-rows: auto auto minmax(0, 1fr);
      }

      .context-panel__header {
        color: var(--color-text-primary);
        font-size: 0.875rem;
        padding-block: 0.5rem;
        padding-inline: 0.75rem;
      }

      .context-panel__close {
        height: 2.75rem;
        width: 2.75rem;
      }

      .context-panel__primary-tabs {
        border-bottom: 1px solid var(--color-border-base);
        display: flex;
        gap: 0.5rem;
        justify-content: center;
        padding: 0.625rem 0.75rem;
      }

      .context-panel__primary-tabs button {
        align-items: center;
        border: 1px solid var(--color-border-base);
        border-radius: 0.625rem;
        color: var(--color-text-secondary);
        display: inline-flex;
        flex: 0 0 auto;
        height: 2.75rem;
        justify-content: center;
        padding: 0;
        width: 2.75rem;
      }

      .context-panel__primary-tab-label {
        border: 0;
        clip: rect(0 0 0 0);
        height: 1px;
        margin: -1px;
        overflow: hidden;
        position: absolute;
        white-space: nowrap;
        width: 1px;
      }

      .context-panel__primary-tabs .context-panel__primary-tab--active {
        background: color-mix(in srgb, var(--color-accent) 14%, transparent);
        border-color: color-mix(in srgb, var(--color-accent) 45%, var(--color-border-base));
        color: var(--color-accent);
      }

      .context-panel__body {
        padding: 0.75rem;
      }
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextPanelComponent {
  readonly context = inject(ContextPanelService);

  activeTabLabel(): string {
    const tab = this.context.activeTab();
    if (tab === 'scratchpad') return 'Scratchpad';
    if (tab === 'memories') return 'Memories';
    if (tab === 'context') return 'Context';
    return 'Tools';
  }

  closePanel(): void {
    this.context.setDesktopVisible(false);
    this.context.closeMobile();
  }
}

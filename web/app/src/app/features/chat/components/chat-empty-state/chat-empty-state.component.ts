import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

@Component({
  selector: 'app-chat-empty-state',
  standalone: true,
  template: `
    <section class="empty" aria-label="No conversation selected">
      <p class="empty__eyebrow">Conversation Core</p>
      <h1>{{ title() }}</h1>
      <p>{{ description() }}</p>
      <div class="empty__actions">
        <button type="button" class="empty__primary" (click)="start.emit()">Start new conversation</button>
        <button type="button" class="empty__secondary" (click)="pickPersona.emit()">Pick a personality…</button>
      </div>
    </section>
  `,
  styles: [`
    .empty {
      align-items: center;
      color: var(--color-text-secondary);
      display: grid;
      gap: 1rem;
      justify-items: center;
      margin: auto;
      max-width: 32rem;
      padding: 2rem;
      text-align: center;
    }

    .empty__eyebrow {
      color: var(--color-accent);
      font-size: 0.75rem;
      font-weight: 700;
      letter-spacing: 0.12em;
      text-transform: uppercase;
    }

    h1 {
      color: var(--color-text-primary);
      font-size: clamp(1.75rem, 4vw, 2.75rem);
      margin: 0;
    }

    .empty__actions {
      display: flex;
      flex-wrap: wrap;
      gap: 0.5rem;
      justify-content: center;
    }

    button {
      border-radius: 999px;
      font-weight: 700;
      padding: 0.75rem 1.25rem;
    }

    .empty__primary {
      background: var(--color-accent);
      color: white;
    }

    .empty__secondary {
      background: transparent;
      border: 1px solid var(--color-border-base);
      color: var(--color-text-secondary);
    }

    .empty__secondary:hover {
      background: var(--color-surface-elevated, var(--color-surface-base));
      color: var(--color-text-primary);
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ChatEmptyStateComponent {
  readonly title = input('Open a conversation');
  readonly description = input('Use the command palette (Cmd+K) to open another chat.');
  readonly start = output<void>();
  readonly pickPersona = output<void>();
}

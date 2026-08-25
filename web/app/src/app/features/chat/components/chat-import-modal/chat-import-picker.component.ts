
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { Chat } from '../../../../core/models/chat.model';
import { relativeTimeCompact } from './chat-import-modal.utils';

@Component({
  selector: 'app-chat-import-picker',
  standalone: true,
  imports: [],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="import__picker">
      <p class="import__picker-lead">
        Pick up to {{ maxSelect() }} threads to bring back now. We'll summarize each and seed key memories so you can pick up right where you left off.
      </p>
      @if (loading()) {
        <div class="import__progress" role="status">
          <div class="import__spinner" aria-hidden="true"></div>
          <p class="import__progress-label">Loading your imported threads…</p>
        </div>
      } @else if (candidates().length === 0) {
        <p class="import__picker-empty">No imported threads to prepare right now.</p>
      } @else {
        <p class="import__picker-count" role="status" aria-live="polite">{{ selectedCount() }} / {{ maxSelect() }} selected</p>
        <ul class="import__picker-list">
          @for (chat of candidates(); track chat.id; let i = $index) {
            <li>
              <label class="import__picker-row" [class.import__picker-row--on]="isSelected(chat.id)" [class.import__picker-row--disabled]="!isSelected(chat.id) && atSelectionLimit()">
                <input
                  type="checkbox"
                  class="import__picker-check"
                  [checked]="isSelected(chat.id)"
                  [disabled]="!isSelected(chat.id) && atSelectionLimit()"
                  (change)="toggle.emit(chat.id)"
                />
                <span class="import__picker-info">
                  <span class="import__picker-name">{{ chat.name }}</span>
                  <span class="import__picker-meta">{{ relativeTime(chat.last_message_time || chat.created_at) }}</span>
                </span>
                @if (i < 3) {
                  <span class="import__picker-badge">Recent</span>
                }
              </label>
            </li>
          }
        </ul>
      }
    </div>
  `,
  styles: [`
    .import__picker { display: flex; flex-direction: column; gap: 0.625rem; }
    .import__picker-lead { color: var(--color-text-secondary); font-size: 0.8125rem; margin: 0; }
    .import__picker-empty { color: var(--color-text-muted); font-size: 0.8125rem; margin: 0.5rem 0; text-align: center; }
    .import__picker-count { color: var(--color-text-muted); font-size: 0.75rem; font-weight: 600; margin: 0; }
    .import__picker-list { display: flex; flex-direction: column; gap: 0.375rem; list-style: none; margin: 0; max-height: 18rem; overflow-y: auto; padding: 0; }
    .import__picker-row {
      align-items: center;
      background: var(--color-surface-muted);
      border: 1.5px solid transparent;
      border-radius: 0.5rem;
      cursor: pointer;
      display: flex;
      gap: 0.625rem;
      padding: 0.5rem 0.75rem;
      transition: border-color 0.15s, background 0.15s;
    }
    .import__picker-row:hover { border-color: var(--color-border-base); }
    .import__picker-row--on { background: color-mix(in srgb, var(--color-accent) 12%, transparent); border-color: var(--color-accent); }
    .import__picker-row--disabled { cursor: not-allowed; opacity: 0.5; }
    .import__picker-check { accent-color: var(--color-accent); flex: none; height: 1rem; width: 1rem; }
    .import__picker-info { display: flex; flex-direction: column; gap: 0.0625rem; min-width: 0; flex: 1; }
    .import__picker-name { color: var(--color-text-primary); font-size: 0.8125rem; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .import__picker-meta { color: var(--color-text-muted); font-size: 0.6875rem; }
    .import__picker-badge { background: color-mix(in srgb, var(--color-accent) 18%, transparent); border-radius: 999px; color: var(--color-accent); flex: none; font-size: 0.625rem; font-weight: 700; padding: 0.125rem 0.5rem; text-transform: uppercase; }

    .import__progress { align-items: center; display: flex; flex-direction: column; gap: 0.75rem; padding: 1.5rem 0; }
    .import__progress-label { color: var(--color-text-primary); font-size: 0.875rem; font-weight: 600; margin: 0; }
    .import__spinner {
      border: 3px solid var(--color-surface-muted);
      border-top-color: var(--color-accent);
      border-radius: 50%;
      height: 1.75rem; width: 1.75rem;
      animation: import-spin 0.8s linear infinite;
    }
    @keyframes import-spin { to { transform: rotate(360deg); } }
  `],
})
export class ChatImportPickerComponent {
  readonly candidates = input.required<Chat[]>();
  readonly loading = input<boolean>(false);
  readonly selectedIds = input<Set<string>>(new Set());
  readonly maxSelect = input<number>(5);
  readonly toggle = output<string>();

  selectedCount(): number {
    return this.selectedIds().size;
  }

  isSelected(id: string): boolean {
    return this.selectedIds().has(id);
  }

  atSelectionLimit(): boolean {
    return this.selectedIds().size >= this.maxSelect();
  }

  relativeTime(iso?: string): string {
    return relativeTimeCompact(iso);
  }
}

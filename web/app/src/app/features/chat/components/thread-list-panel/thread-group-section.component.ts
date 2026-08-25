import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { Chat } from '../../../../core/models/chat.model';
import { ThreadGroup } from '../../helpers/thread-list.helpers';
import { ThreadRowComponent } from './thread-row.component';

@Component({
  selector: 'app-thread-group-section',
  standalone: true,
  imports: [ThreadRowComponent],
  template: `
    <section class="group" [attr.aria-label]="group().label">
      <h3>{{ group().label }}</h3>
      <div class="group__rows">
        @for (thread of group().threads; track thread.id) {
          <app-thread-row
            [thread]="thread"
            [active]="activeThreadId() === thread.id"
            (select)="select.emit($event)"
            (rename)="rename.emit($event)"
            (togglePin)="togglePin.emit($event)"
            (editTags)="editTags.emit($event)"
            (deleteThread)="deleteThread.emit($event)"
          />
        }
      </div>
    </section>
  `,
  styles: [`
    .group {
      display: grid;
      gap: 0.5rem;
    }

    h3 {
      color: var(--color-text-muted);
      font-size: 0.75rem;
      font-weight: 700;
      letter-spacing: 0.08em;
      margin: 0;
      text-transform: uppercase;
    }

    .group__rows {
      display: grid;
      gap: 0.35rem;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadGroupSectionComponent {
  readonly group = input.required<ThreadGroup>();
  readonly activeThreadId = input<string | null>(null);

  readonly select = output<string>();
  readonly rename = output<{ thread: Chat; name: string }>();
  readonly togglePin = output<Chat>();
  readonly editTags = output<Chat>();
  readonly deleteThread = output<Chat>();
}

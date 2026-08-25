import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';

import { RitualService } from '../../../core/services/ritual.service';
import { Ritual } from '../../../core/models/ritual.model';

@Component({
  selector: 'app-ritual-available-preview',
  standalone: true,
  templateUrl: './ritual-available-preview.component.html',
  styleUrl: './ritual-available-preview.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class RitualAvailablePreviewComponent {
  private readonly ritualService = inject(RitualService);

  readonly ritualId = input<string | null>(null);
  readonly chatId = input<string>('');

  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly availableRituals = signal<Ritual[]>([]);
  readonly availableInChat = signal<boolean | null>(null);

  ngOnChanges(): void {
    const chatId = this.chatId();
    if (!chatId) {
      this.availableRituals.set([]);
      this.availableInChat.set(null);
      this.error.set(null);
      return;
    }

    this.loading.set(true);
    this.error.set(null);
    this.ritualService.getAvailableRituals(chatId, 1, 100).subscribe({
      next: response => {
        const rituals = response.results ?? [];
        this.availableRituals.set(rituals);
        const ritualId = this.ritualId();
        this.availableInChat.set(ritualId ? rituals.some(ritual => ritual.id === ritualId) : null);
        this.loading.set(false);
      },
      error: error => {
        this.loading.set(false);
        this.availableRituals.set([]);
        this.availableInChat.set(null);
        this.error.set(error instanceof Error ? error.message : 'Failed to load available skills');
      },
    });
  }
}

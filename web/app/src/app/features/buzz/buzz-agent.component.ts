import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { firstValueFrom } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { BuzzAgent, BuzzService } from './buzz.service';
import { BuzzAddCommunityComponent } from './buzz-add-community.component';
import { BuzzCommunityComponent } from './buzz-community.component';
import { BuzzTabState } from './buzz-tab.state';
import { BUZZ_UI, buzzStatusClass, buzzStatusLabel } from './buzz-ui';

/** One persona's Buzz identity: its npub, pause, profile, remove, and the communities it is in. */
@Component({
  selector: 'app-buzz-agent',
  standalone: true,
  imports: [DatePipe, BuzzAddCommunityComponent, BuzzCommunityComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let a = agent();
    <section class="rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800" data-testid="buzz-agent">
      <div class="flex flex-wrap items-start justify-between gap-3 p-4">
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ a.persona_name || 'A persona' }}</h3>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span [class]="ui.chip + ' ' + statusClass(a.status)">{{ statusLabel(a.status) }}</span>
            <span>Added {{ a.created_at | date: 'mediumDate' }}</span>
          </div>
          <div class="mt-2 flex flex-wrap items-center gap-2">
            <code
              class="break-all rounded bg-gray-100 px-2 py-1 text-xs text-gray-800 dark:bg-gray-900 dark:text-gray-200"
              data-testid="buzz-agent-npub"
              >{{ a.npub }}</code
            >
            <button type="button" [class]="ui.secondary" (click)="copyNpub()" data-testid="buzz-agent-copy">
              {{ copied() ? 'Copied' : 'Copy' }}
            </button>
          </div>
          <p class="mt-1" [class]="ui.hint">Share this with a community admin, or use an invite link.</p>
          @if (a.last_error) {
            <p [class]="ui.error">{{ a.last_error }}</p>
          }
        </div>
        <div class="flex flex-wrap gap-2">
          <button type="button" [class]="ui.secondary" [disabled]="state.busy()" (click)="syncProfile()" data-testid="buzz-agent-sync">
            Use persona's name and portrait
          </button>
          <button type="button" [class]="ui.secondary" [disabled]="state.busy()" (click)="togglePaused()" data-testid="buzz-agent-pause">
            {{ a.status === 'disabled' ? 'Resume' : 'Pause' }}
          </button>
          <button type="button" [class]="ui.danger" [disabled]="state.busy()" (click)="remove()" data-testid="buzz-agent-remove">
            Remove
          </button>
        </div>
      </div>

      @for (c of a.communities; track c.id) {
        <app-buzz-community [community]="c" [agent]="a" />
      }
      <div class="border-t border-gray-200 p-4 dark:border-gray-700">
        <app-buzz-add-community [agent]="a" />
      </div>
    </section>
  `,
})
export class BuzzAgentComponent {
  private readonly buzz = inject(BuzzService);
  private readonly confirm = inject(ConfirmationService);
  readonly state = inject(BuzzTabState);

  readonly agent = input.required<BuzzAgent>();

  readonly ui = BUZZ_UI;
  readonly statusClass = buzzStatusClass;
  readonly statusLabel = buzzStatusLabel;
  readonly copied = signal(false);

  async copyNpub(): Promise<void> {
    try {
      await navigator.clipboard.writeText(this.agent().npub);
      this.copied.set(true);
      setTimeout(() => this.copied.set(false), 2000);
    } catch {
      this.state.error.set('Could not copy automatically. Select the npub and copy it.');
    }
  }

  async syncProfile(): Promise<void> {
    await this.state.run(async () => {
      const res = await firstValueFrom(this.buzz.syncProfile(this.agent().id));
      if (res.failed.length) {
        this.state.error.set(
          `Published to ${res.published} communit${res.published === 1 ? 'y' : 'ies'}; not to ${res.failed.join(', ')}.`,
        );
      } else {
        this.state.notice.set(
          res.published
            ? `Name and portrait published to ${res.published} communit${res.published === 1 ? 'y' : 'ies'}.`
            : 'No active communities to publish to yet.',
        );
      }
    }, 'Could not publish the persona’s profile.');
  }

  async togglePaused(): Promise<void> {
    await this.state.run(
      async () =>
        this.state.replaceAgent(await firstValueFrom(this.buzz.setAgentEnabled(this.agent().id, this.agent().status === 'disabled'))),
      'Could not update the Buzz identity.',
    );
  }

  async remove(): Promise<void> {
    const ok = await this.confirm.confirm({
      title: 'Remove Buzz identity',
      message:
        `This deletes ${this.agent().persona_name || 'the persona'}'s Buzz key for good and disconnects all of its channels; the relay threads stay in your thread list. ` +
        'Connecting the persona again makes a new key, which Buzz sees as a new member. Nothing is deleted in Buzz.',
      confirmText: 'Remove',
      type: 'danger',
    });
    if (!ok) return;
    await this.state.run(async () => {
      await firstValueFrom(this.buzz.removeAgent(this.agent().id));
      this.state.removeAgent(this.agent().id);
    }, 'Could not remove the Buzz identity.');
  }
}

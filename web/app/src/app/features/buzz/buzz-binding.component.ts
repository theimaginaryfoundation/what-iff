import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import {
  BUZZ_EMPTY_ALLOW_NOTE,
  BUZZ_UNRESTRICTED_WARNING,
  BuzzBinding,
  BuzzService,
  UpdateBuzzBindingRequest,
  buzzBindingLabel,
} from './buzz.service';
import { BuzzPubkeyChipsComponent } from './buzz-pubkey-chips.component';
import { BuzzTabState } from './buzz-tab.state';
import { BUZZ_UI, buzzStatusClass } from './buzz-ui';

/** Editable access lists (hex pubkeys) for one binding, until saved. */
interface AccessDraft {
  allow: string[];
  deny: string[];
}

/**
 * One connected channel: its status, whether the relay thread is sandboxed, whether mentions are
 * answered (and from other agents), who may mention the persona, and disconnect.
 */
@Component({
  selector: 'app-buzz-binding',
  standalone: true,
  imports: [DatePipe, RouterLink, BuzzPubkeyChipsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let b = binding();
    <div class="border-b border-gray-100 p-4 last:border-b-0 dark:border-gray-700/60" data-testid="buzz-binding">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="text-sm font-medium text-gray-900 dark:text-white">{{ label() }}</div>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span [class]="ui.chip + ' ' + statusClass(b.status)">{{ b.status === 'active' ? 'Connected' : 'Broken' }}</span>
            @if (b.chat_sandboxed === true) {
              <span [class]="ui.chip + ' ' + statusClass('active')" data-testid="buzz-binding-sandboxed">Sandboxed thread</span>
            } @else if (b.chat_sandboxed === false) {
              <span [class]="ui.chip + ' ' + statusClass('needs_access')" data-testid="buzz-binding-unsandboxed">
                {{ b.allow_unrestricted ? 'Not sandboxed (acknowledged)' : 'Not sandboxed: paused' }}
              </span>
            }
            @if (!b.allow_pubkeys.length) {
              <span [class]="ui.chip + ' ' + statusClass('disabled')" data-testid="buzz-binding-open-to-anyone"
                >Open to anyone in the channel</span
              >
            }
            <a [class]="ui.link" [routerLink]="['/chat', b.chat_id]">Open relay thread</a>
            @if (b.last_activity_at) {
              <span>Last activity {{ b.last_activity_at | date: 'short' }}</span>
            }
          </div>
          @if (b.last_error) {
            <p [class]="ui.error">{{ b.last_error }}</p>
          }
          @if (b.chat_sandboxed === false) {
            <label class="mt-2 flex max-w-xl items-start gap-2 text-xs text-amber-900 dark:text-amber-200">
              <input
                type="checkbox"
                class="mt-0.5"
                [checked]="b.allow_unrestricted"
                [disabled]="state.busy()"
                (change)="update({ allow_unrestricted: $any($event.target).checked })"
                data-testid="buzz-binding-ack"
              />
              <span>This thread is not sandboxed. {{ unrestrictedWarning }} Until this is ticked, mentions here are not answered.</span>
            </label>
          }
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <label class="inline-flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300">
            <input
              type="checkbox"
              [checked]="b.inbound_enabled"
              [disabled]="state.busy()"
              (change)="update({ inbound_enabled: $any($event.target).checked })"
              data-testid="buzz-binding-inbound"
            />
            Answer when mentioned
          </label>
          @if (b.status === 'broken') {
            <button
              type="button"
              [class]="ui.secondary"
              [disabled]="state.busy()"
              (click)="update({ reactivate: true })"
              data-testid="buzz-binding-reactivate"
            >
              Try again
            </button>
          }
          <button type="button" [class]="ui.danger" [disabled]="state.busy()" (click)="disconnect()" data-testid="buzz-binding-remove">
            Disconnect
          </button>
        </div>
      </div>
      <label class="mt-2 flex max-w-xl items-start gap-2 text-xs text-gray-700 dark:text-gray-300">
        <input
          type="checkbox"
          class="mt-0.5"
          [checked]="b.answer_agents"
          [disabled]="state.busy()"
          (change)="update({ answer_agents: $any($event.target).checked })"
          data-testid="buzz-binding-agents"
        />
        <span>
          Answer other agents
          <span class="block text-gray-500 dark:text-gray-400">
            Off by default. Agents that mention each other are not yet stopped from looping, so turn this on only for agents you trust to
            stop.
          </span>
        </span>
      </label>
      <details class="mt-3 text-xs text-gray-700 dark:text-gray-300">
        <summary class="cursor-pointer select-none">Who can mention the persona here</summary>
        <p class="mt-2 text-gray-500 dark:text-gray-400">
          Buzz public keys (npub or hex). Press Enter, comma or Space after each key, or paste a whole list. An empty allow list means
          anyone in the channel, and deny always wins. Each answer is a turn on your account, in a channel other people can read.
        </p>
        @if (!draft().allow.length) {
          <p class="mt-2 font-medium text-amber-800 dark:text-amber-300" data-testid="buzz-empty-allow-note">
            Allow list is empty: {{ emptyAllowNote }}
          </p>
        }
        <div class="mt-2 grid grid-cols-1 gap-3 md:grid-cols-2">
          <div>
            <span class="mb-1 block text-gray-600 dark:text-gray-400">Allow</span>
            <app-buzz-pubkey-chips
              label="Allowed Buzz public keys"
              placeholder="Anyone, until you add a key…"
              [keys]="draft().allow"
              (keysChange)="setDraft('allow', $event)"
            />
          </div>
          <div>
            <span class="mb-1 block text-gray-600 dark:text-gray-400">Deny</span>
            <app-buzz-pubkey-chips
              label="Denied Buzz public keys"
              placeholder="Nobody blocked…"
              [keys]="draft().deny"
              (keysChange)="setDraft('deny', $event)"
            />
          </div>
        </div>
        <button
          type="button"
          class="mt-2"
          [class]="ui.primary"
          [disabled]="state.busy()"
          (click)="saveAccess()"
          data-testid="buzz-binding-save-access"
        >
          Save
        </button>
      </details>
    </div>
  `,
})
export class BuzzBindingComponent {
  private readonly buzz = inject(BuzzService);
  private readonly confirm = inject(ConfirmationService);
  readonly state = inject(BuzzTabState);

  readonly binding = input.required<BuzzBinding>();
  readonly communityName = input<string | undefined>(undefined);

  readonly ui = BUZZ_UI;
  readonly statusClass = buzzStatusClass;
  readonly unrestrictedWarning = BUZZ_UNRESTRICTED_WARNING;
  readonly emptyAllowNote = BUZZ_EMPTY_ALLOW_NOTE;
  private readonly edited = signal<AccessDraft | null>(null);

  label(): string {
    return buzzBindingLabel(this.binding(), this.communityName());
  }

  draft(): AccessDraft {
    return this.edited() ?? { allow: this.binding().allow_pubkeys, deny: this.binding().deny_pubkeys };
  }

  setDraft(key: keyof AccessDraft, keys: string[]): void {
    this.edited.set({ ...this.draft(), [key]: keys });
  }

  async update(body: UpdateBuzzBindingRequest): Promise<void> {
    await this.state.run(
      async () => this.state.putBinding(await firstValueFrom(this.buzz.updateBinding(this.binding().id, body))),
      'Could not update the channel.',
    );
  }

  async saveAccess(): Promise<void> {
    const d = this.draft();
    const ok = await this.state.run(
      async () =>
        this.state.putBinding(
          await firstValueFrom(this.buzz.updateBinding(this.binding().id, { allow_pubkeys: d.allow, deny_pubkeys: d.deny })),
        ),
      'Could not save who can mention the persona.',
    );
    if (ok) this.edited.set(null);
  }

  async disconnect(): Promise<void> {
    const ok = await this.confirm.confirm({
      title: 'Disconnect channel',
      message: `Stop relaying ${this.label()}? The relay thread stays in your thread list.`,
      confirmText: 'Disconnect',
      type: 'danger',
    });
    if (!ok) return;
    await this.state.run(async () => {
      await firstValueFrom(this.buzz.removeBinding(this.binding().id));
      this.state.removeBinding(this.binding().id);
    }, 'Could not disconnect the channel.');
  }
}

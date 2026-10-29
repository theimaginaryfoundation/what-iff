import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { RouterLink } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { saveCheckbox } from '../../core/utils/checkbox.helpers';
import { RELAY_EMPTY_ALLOW_NOTE, RELAY_UNRESTRICTED_WARNING } from '../relay/relay-copy';
import { RelayIdChipsComponent } from '../relay/relay-id-chips.component';
import { RELAY_TONE_CLASS, RELAY_UI } from '../relay/relay-ui';
import { SLACK_USER_IDS, SlackBinding, SlackService, UpdateSlackBindingRequest, slackBindingLabel } from './slack.service';
import { SlackTabState } from './slack-tab.state';
import { slackStatusClass } from './slack-ui';

/** Editable access lists (Slack member ids) for one binding, until saved. */
interface AccessDraft {
  allow: string[];
  deny: string[];
}

/**
 * One connected channel: its status, whether the relay thread is sandboxed, whether mentions are
 * answered (and from other apps' bots), who may mention the persona, and disconnect.
 */
type BindingFlag = 'allow_unrestricted' | 'inbound_enabled' | 'answer_agents';

@Component({
  selector: 'app-slack-binding',
  standalone: true,
  imports: [DatePipe, RouterLink, RelayIdChipsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let b = binding();
    <div class="border-b border-gray-100 p-4 last:border-b-0 dark:border-gray-700/60" data-testid="slack-binding">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div class="min-w-0">
          <div class="text-sm font-medium text-gray-900 dark:text-white">
            {{ label() }}
            @if (b.is_private) {
              <span class="ml-1 font-normal" [class]="ui.hint">private</span>
            }
          </div>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span [class]="ui.chip + ' ' + statusClass(b.status)" data-testid="slack-binding-status">{{
              b.status === 'active' ? 'Connected' : 'Broken'
            }}</span>
            @if (b.chat_sandboxed === true) {
              <span [class]="ui.chip + ' ' + tone.good" data-testid="slack-binding-sandboxed">Sandboxed thread</span>
            } @else if (b.chat_sandboxed === false) {
              <span [class]="ui.chip + ' ' + tone.warn" data-testid="slack-binding-unsandboxed">
                {{ b.allow_unrestricted ? 'Not sandboxed (acknowledged)' : 'Not sandboxed: paused' }}
              </span>
            }
            @if (!b.allow_user_ids.length) {
              <span [class]="ui.chip + ' ' + tone.paused" data-testid="slack-binding-open-to-anyone">Open to anyone in the channel</span>
            }
            <a [class]="ui.link" [routerLink]="['/chat', b.chat_id]" data-testid="slack-binding-open-thread">Open relay thread</a>
            @if (b.last_activity_at) {
              <span>Last activity {{ b.last_activity_at | date: 'short' }}</span>
            }
          </div>
          @if (b.last_error) {
            <p [class]="ui.error" data-testid="slack-binding-error">{{ b.last_error }}</p>
          }
          @if (b.chat_sandboxed === false) {
            <label class="mt-2 flex max-w-xl items-start gap-2 text-xs text-amber-900 dark:text-amber-200">
              <input
                type="checkbox"
                class="mt-0.5"
                [checked]="b.allow_unrestricted"
                [disabled]="state.busy()"
                (change)="setFlag('allow_unrestricted', $any($event.target))"
                data-testid="slack-binding-ack"
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
              (change)="setFlag('inbound_enabled', $any($event.target))"
              data-testid="slack-binding-inbound"
            />
            Answer when mentioned
          </label>
          @if (b.status === 'broken') {
            <button
              type="button"
              [class]="ui.secondary"
              [disabled]="state.busy()"
              (click)="update({ reactivate: true })"
              data-testid="slack-binding-reactivate"
            >
              Reactivate
            </button>
          }
          <button type="button" [class]="ui.danger" [disabled]="state.busy()" (click)="disconnect()" data-testid="slack-binding-remove">
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
          (change)="setFlag('answer_agents', $any($event.target))"
          data-testid="slack-binding-agents"
        />
        <span>
          Answer other apps' bots
          <span class="block text-gray-500 dark:text-gray-400">
            Off by default. Bots may mention the persona a few times in a row; then it wraps up, and a person has to speak before it answers
            bots again.
          </span>
        </span>
      </label>
      <details class="mt-3 text-xs text-gray-700 dark:text-gray-300">
        <summary class="cursor-pointer select-none">Who can mention the persona here</summary>
        <p class="mt-2 text-gray-500 dark:text-gray-400">
          Slack member ids (open a profile, ⋮, Copy member ID). Press Enter, comma or Space after each id, or paste a whole list. An empty
          allow list means anyone in the channel, and deny always wins. Each answer is a turn on your account, in a channel other people can
          read.
        </p>
        @if (!draft().allow.length) {
          <p class="mt-2 font-medium text-amber-800 dark:text-amber-300" data-testid="slack-empty-allow-note">
            Allow list is empty: {{ emptyAllowNote }}
          </p>
        }
        <div class="mt-2 grid grid-cols-1 gap-3 md:grid-cols-2">
          <div>
            <span class="mb-1 block text-gray-600 dark:text-gray-400">Allow</span>
            <app-relay-id-chips
              [kind]="userIds"
              label="Allowed Slack member ids"
              placeholder="Anyone, until you add an id…"
              [ids]="draft().allow"
              (idsChange)="setDraft('allow', $event)"
              data-testid="slack-binding-allow"
            />
          </div>
          <div>
            <span class="mb-1 block text-gray-600 dark:text-gray-400">Deny</span>
            <app-relay-id-chips
              [kind]="userIds"
              label="Denied Slack member ids"
              placeholder="Nobody blocked…"
              [ids]="draft().deny"
              (idsChange)="setDraft('deny', $event)"
              data-testid="slack-binding-deny"
            />
          </div>
        </div>
        <button
          type="button"
          class="mt-2"
          [class]="ui.primary"
          [disabled]="state.busy()"
          (click)="saveAccess()"
          data-testid="slack-binding-save-access"
        >
          Save
        </button>
      </details>
    </div>
  `,
})
export class SlackBindingComponent {
  private readonly slack = inject(SlackService);
  private readonly confirm = inject(ConfirmationService);
  readonly state = inject(SlackTabState);

  readonly binding = input.required<SlackBinding>();
  readonly teamName = input<string | undefined>(undefined);

  readonly ui = RELAY_UI;
  readonly tone = RELAY_TONE_CLASS;
  readonly statusClass = slackStatusClass;
  readonly userIds = SLACK_USER_IDS;
  readonly unrestrictedWarning = RELAY_UNRESTRICTED_WARNING;
  readonly emptyAllowNote = RELAY_EMPTY_ALLOW_NOTE;
  private readonly edited = signal<AccessDraft | null>(null);

  label(): string {
    return slackBindingLabel(this.binding(), this.teamName());
  }

  draft(): AccessDraft {
    return this.edited() ?? { allow: this.binding().allow_user_ids, deny: this.binding().deny_user_ids };
  }

  setDraft(key: keyof AccessDraft, ids: string[]): void {
    this.edited.set({ ...this.draft(), [key]: ids });
  }

  /** Saves one of the binding's switches, putting the box back when the save fails (saveCheckbox). */
  async setFlag(key: BindingFlag, box: HTMLInputElement): Promise<void> {
    await saveCheckbox(box, on => this.update({ [key]: on }));
  }

  async update(body: UpdateSlackBindingRequest): Promise<boolean> {
    return this.state.run(
      async () => this.state.putBinding(await firstValueFrom(this.slack.updateBinding(this.binding().id, body))),
      'Could not update the channel.',
    );
  }

  async saveAccess(): Promise<void> {
    const d = this.draft();
    const ok = await this.state.run(
      async () =>
        this.state.putBinding(
          await firstValueFrom(this.slack.updateBinding(this.binding().id, { allow_user_ids: d.allow, deny_user_ids: d.deny })),
        ),
      'Could not save who can mention the persona.',
    );
    if (ok) this.edited.set(null);
  }

  async disconnect(): Promise<void> {
    const ok = await this.confirm.confirm({
      title: 'Disconnect channel',
      message: `Stop relaying ${this.label()}? The relay thread stays in your thread list, and the app stays in the channel in Slack.`,
      confirmText: 'Disconnect',
      type: 'danger',
    });
    if (!ok) return;
    await this.state.run(async () => {
      await firstValueFrom(this.slack.removeBinding(this.binding().id));
      this.state.removeBinding(this.binding().id);
    }, 'Could not disconnect the channel.');
  }
}

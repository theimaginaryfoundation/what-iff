import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { RELAY_UI } from '../relay/relay-ui';
import {
  SLACK_APP_TOKEN_PREFIX,
  SLACK_BOT_TOKEN_PREFIX,
  SlackApp,
  SlackService,
  UpdateSlackAppRequest,
  tokenLooksWrong,
} from './slack.service';
import { SlackBindingComponent } from './slack-binding.component';
import { SlackChannelsComponent } from './slack-channels.component';
import { SlackTabState } from './slack-tab.state';
import { slackStatusClass, slackStatusLabel } from './slack-ui';

/**
 * One persona's Slack app in one workspace: its bot, status, the link to its settings in Slack
 * (where the portrait is uploaded), replace tokens, refresh, pause, remove, and its connected
 * channels with the channel list to connect more.
 */
@Component({
  selector: 'app-slack-app',
  standalone: true,
  imports: [DatePipe, FormsModule, SlackBindingComponent, SlackChannelsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let a = app();
    <section class="rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800" data-testid="slack-app">
      <div class="flex flex-wrap items-start justify-between gap-3 p-4">
        <div class="min-w-0">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
            {{ personaName() }}
            <span class="font-normal text-gray-500 dark:text-gray-400">in {{ a.team_name || a.team_id }}</span>
          </h3>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span [class]="ui.chip + ' ' + statusClass(a.status)" data-testid="slack-app-status">{{ statusLabel(a.status) }}</span>
            <span data-testid="slack-app-bot">&#64;{{ a.bot_name }}</span>
            @if (a.team_url) {
              <a [class]="ui.link" [href]="a.team_url" target="_blank" rel="noopener">Open the workspace</a>
            }
            @if (a.last_connected_at) {
              <span>Last connected {{ a.last_connected_at | date: 'short' }}</span>
            }
          </div>
          @if (a.last_error) {
            <p [class]="ui.error" data-testid="slack-app-error">{{ a.last_error }}</p>
          }
          <p class="mt-1" [class]="ui.hint">
            To give &#64;{{ a.bot_name }} {{ personaName() }}'s portrait, upload it in the app's settings in Slack, under Basic Information
            → Display Information.
          </p>
        </div>
        <div class="flex flex-wrap gap-2">
          <a [class]="ui.secondary" [href]="a.settings_url" target="_blank" rel="noopener" data-testid="slack-app-settings">
            App settings in Slack
          </a>
          <button
            type="button"
            [class]="ui.secondary"
            [disabled]="state.busy()"
            (click)="toggleTokens()"
            data-testid="slack-app-tokens-open"
          >
            Replace tokens
          </button>
          <button
            type="button"
            [class]="ui.secondary"
            [disabled]="state.busy()"
            (click)="update({ refresh: true }, 'Could not refresh the app from Slack.')"
            data-testid="slack-app-refresh"
          >
            Refresh
          </button>
          <!-- Not while Slack refuses the tokens: replacing them is the way out, and resuming would
               only show "Active" over tokens that still do not work. -->
          @if (a.status !== 'invalid_token') {
            <button
              type="button"
              [class]="ui.secondary"
              [disabled]="state.busy()"
              (click)="update({ enabled: a.status === 'disabled' }, 'Could not update the Slack app.')"
              data-testid="slack-app-pause"
            >
              {{ a.status === 'disabled' ? 'Resume' : 'Pause' }}
            </button>
          }
          <button type="button" [class]="ui.danger" [disabled]="state.busy()" (click)="remove()" data-testid="slack-app-remove">
            Remove
          </button>
        </div>
      </div>

      @if (editingTokens()) {
        <div class="max-w-3xl px-4 pb-4" data-testid="slack-app-tokens-form">
          <p [class]="ui.hint">
            Paste either or both. They must belong to this same app; leave a field empty to keep the token What Iff has.
          </p>
          <div class="mt-2 grid grid-cols-1 gap-3 md:grid-cols-2">
            <label class="block text-xs text-gray-600 dark:text-gray-400">
              Bot User OAuth Token
              <input
                type="password"
                autocomplete="off"
                class="mt-1"
                [class]="ui.input"
                [placeholder]="botPrefix + '…'"
                [ngModel]="botToken()"
                (ngModelChange)="botToken.set($event)"
                data-testid="slack-app-bot-token"
              />
              @if (botTokenWrong()) {
                <span [class]="ui.error">This one starts with {{ botPrefix }}</span>
              }
            </label>
            <label class="block text-xs text-gray-600 dark:text-gray-400">
              App-Level Token
              <input
                type="password"
                autocomplete="off"
                class="mt-1"
                [class]="ui.input"
                [placeholder]="appPrefix + '…'"
                [ngModel]="appToken()"
                (ngModelChange)="appToken.set($event)"
                data-testid="slack-app-app-token"
              />
              @if (appTokenWrong()) {
                <span [class]="ui.error">This one starts with {{ appPrefix }}</span>
              }
            </label>
          </div>
          <div class="mt-2 flex gap-2">
            <button
              type="button"
              [class]="ui.primary"
              [disabled]="!canSaveTokens() || state.busy()"
              (click)="saveTokens()"
              data-testid="slack-app-tokens-save"
            >
              Save tokens
            </button>
            <button type="button" [class]="ui.secondary" (click)="toggleTokens()">Cancel</button>
          </div>
        </div>
      }

      <div class="border-t border-gray-200 dark:border-gray-700">
        @for (b of bindings(); track b.id) {
          <app-slack-binding [binding]="b" [teamName]="a.team_name" />
        }
        @if (a.status === 'active') {
          <div class="p-4">
            <app-slack-channels [app]="a" />
          </div>
        } @else {
          <p class="p-4" [class]="ui.hint">
            {{
              a.status === 'disabled'
                ? 'Paused: mentions in Slack are not answered and nothing is posted. Resume to connect channels.'
                : 'Slack no longer accepts this app’s tokens. Replace them, then connect channels.'
            }}
          </p>
        }
      </div>
    </section>
  `,
})
export class SlackAppComponent {
  private readonly slack = inject(SlackService);
  private readonly confirm = inject(ConfirmationService);
  readonly state = inject(SlackTabState);

  readonly app = input.required<SlackApp>();

  readonly ui = RELAY_UI;
  readonly statusClass = slackStatusClass;
  readonly statusLabel = slackStatusLabel;
  readonly botPrefix = SLACK_BOT_TOKEN_PREFIX;
  readonly appPrefix = SLACK_APP_TOKEN_PREFIX;

  readonly editingTokens = signal(false);
  readonly botToken = signal('');
  readonly appToken = signal('');

  readonly personaName = computed(() => this.state.personaName(this.app().personality_id));
  readonly bindings = computed(() => this.state.bindingsFor(this.app().id));
  readonly botTokenWrong = computed(() => tokenLooksWrong(this.botToken(), SLACK_BOT_TOKEN_PREFIX));
  readonly appTokenWrong = computed(() => tokenLooksWrong(this.appToken(), SLACK_APP_TOKEN_PREFIX));
  readonly canSaveTokens = computed(
    () => (!!this.botToken().trim() || !!this.appToken().trim()) && !this.botTokenWrong() && !this.appTokenWrong(),
  );

  toggleTokens(): void {
    this.editingTokens.update(v => !v);
    this.botToken.set('');
    this.appToken.set('');
  }

  async update(body: UpdateSlackAppRequest, fallback: string): Promise<boolean> {
    return this.state.run(async () => this.state.putApp(await firstValueFrom(this.slack.updateApp(this.app().id, body))), fallback);
  }

  async saveTokens(): Promise<void> {
    const body: UpdateSlackAppRequest = {};
    if (this.botToken().trim()) body.bot_token = this.botToken().trim();
    if (this.appToken().trim()) body.app_token = this.appToken().trim();
    if (await this.update(body, 'Could not replace the tokens.')) this.toggleTokens();
  }

  async remove(): Promise<void> {
    const a = this.app();
    const ok = await this.confirm.confirm({
      title: 'Remove Slack app',
      message:
        `Disconnect ${this.personaName()} from ${a.team_name || 'this workspace'}? What Iff forgets the app's tokens and stops relaying its channels; ` +
        'the relay threads stay in your thread list. Nothing is deleted in Slack: remove the app there too if you no longer want it.',
      confirmText: 'Remove',
      type: 'danger',
    });
    if (!ok) return;
    await this.state.run(async () => {
      await firstValueFrom(this.slack.removeApp(a.id));
      this.state.removeApp(a.id);
    }, 'Could not remove the Slack app.');
  }
}

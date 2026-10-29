import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { RELAY_UI } from '../relay/relay-ui';
import { SLACK_APP_TOKEN_PREFIX, SLACK_BOT_TOKEN_PREFIX, SlackManifest, SlackService, tokenLooksWrong } from './slack.service';
import { SlackTabState } from './slack-tab.state';

/**
 * "Connect a persona": choose a persona, create its Slack app from the manifest link, install it,
 * and paste its two tokens. Each persona can have one app per workspace, so every persona is
 * offered; the server refuses a second app in the same workspace (409) with its reason.
 */
@Component({
  selector: 'app-slack-connect',
  standalone: true,
  imports: [FormsModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <section class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800" data-testid="slack-connect">
      <h3 class="text-sm font-semibold text-gray-900 dark:text-white">Connect a persona</h3>
      <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
        The persona gets a Slack app of its own, so it is a member of the workspace with its own name: people mention it and invite it to
        channels. You create the app in Slack from a ready-made setup, then give What Iff its two tokens.
      </p>
      <div class="mt-3 flex max-w-xl flex-wrap gap-3">
        <select
          class="min-w-0 flex-1 rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900 dark:text-white"
          [ngModel]="personaId()"
          (ngModelChange)="pickPersona($event)"
          aria-label="Persona"
          data-testid="slack-persona-select"
        >
          <option value="">Choose a persona…</option>
          @for (p of state.personas(); track p.id) {
            <option [value]="p.id">{{ p.name }}</option>
          }
        </select>
      </div>

      @if (personaId()) {
        <ol class="mt-4 list-decimal space-y-3 pl-5 text-sm text-gray-700 dark:text-gray-300" data-testid="slack-steps">
          <li>
            <div class="flex flex-wrap items-center gap-2">
              @if (manifest(); as m) {
                <a
                  class="rounded-lg bg-indigo-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-indigo-700"
                  [href]="m.create_url"
                  target="_blank"
                  rel="noopener"
                  data-testid="slack-create-app"
                  >Create the Slack app</a
                >
              } @else if (manifestLoading()) {
                <span [class]="ui.hint">Preparing the app…</span>
              }
            </div>
            <p class="mt-1">
              Slack opens with the app filled in: {{ personaName() }}'s name, Socket Mode, and the permissions the relay needs. Pick the
              workspace and choose <strong>Create</strong>.
            </p>
            @if (manifest(); as m) {
              <details class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                <summary class="cursor-pointer select-none">What the app may do</summary>
                <p class="mt-1">Permissions: {{ m.scopes.join(', ') }}</p>
                <p class="mt-1">Events: {{ m.events.join(', ') }}</p>
              </details>
            }
          </li>
          <li>
            On the app's <strong>Install App</strong> page, choose <strong>Install to Workspace</strong> and allow it. Some workspaces send
            this to an admin for approval first.
          </li>
          <li>
            On <strong>OAuth &amp; Permissions</strong>, copy the <strong>Bot User OAuth Token</strong> (it starts with
            <code>{{ botPrefix }}</code
            >).
          </li>
          <li>
            On <strong>Basic Information</strong>, under <strong>App-Level Tokens</strong>, choose
            <strong>Generate Token and Scopes</strong>, add the <code>connections:write</code> scope, and copy the token (it starts with
            <code>{{ appPrefix }}</code
            >).
          </li>
          <li>Paste both here. What Iff checks them with Slack and stores them encrypted; they are never shown again.</li>
        </ol>

        <div class="mt-3 grid max-w-3xl grid-cols-1 gap-3 md:grid-cols-2">
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
              data-testid="slack-bot-token"
            />
            @if (botTokenWrong()) {
              <span [class]="ui.error" data-testid="slack-bot-token-hint">This one starts with {{ botPrefix }}</span>
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
              data-testid="slack-app-token"
            />
            @if (appTokenWrong()) {
              <span [class]="ui.error" data-testid="slack-app-token-hint">This one starts with {{ appPrefix }}</span>
            }
          </label>
        </div>
        <button
          type="button"
          class="mt-3"
          [class]="ui.primary"
          [disabled]="!canConnect() || state.busy()"
          (click)="connect()"
          data-testid="slack-connect-submit"
        >
          Connect {{ personaName() }}
        </button>
        <p class="mt-3" [class]="ui.hint" data-testid="slack-connect-notes">
          A free Slack workspace allows 10 apps and integrations in all, and each persona uses one. Slack does not let an app set its own
          picture: once connected, upload the persona's portrait in the app's settings in Slack (Basic Information → Display Information).
        </p>
      }
    </section>
  `,
})
export class SlackConnectComponent {
  private readonly slack = inject(SlackService);
  readonly state = inject(SlackTabState);

  readonly ui = RELAY_UI;
  readonly botPrefix = SLACK_BOT_TOKEN_PREFIX;
  readonly appPrefix = SLACK_APP_TOKEN_PREFIX;

  readonly personaId = signal('');
  readonly manifest = signal<SlackManifest | null>(null);
  readonly manifestLoading = signal(false);
  readonly botToken = signal('');
  readonly appToken = signal('');

  readonly personaName = computed(() => this.state.personaName(this.personaId()));
  readonly botTokenWrong = computed(() => tokenLooksWrong(this.botToken(), SLACK_BOT_TOKEN_PREFIX));
  readonly appTokenWrong = computed(() => tokenLooksWrong(this.appToken(), SLACK_APP_TOKEN_PREFIX));
  readonly canConnect = computed(
    () => !!this.personaId() && !!this.botToken().trim() && !!this.appToken().trim() && !this.botTokenWrong() && !this.appTokenWrong(),
  );

  async pickPersona(id: string): Promise<void> {
    this.personaId.set(id);
    this.manifest.set(null);
    if (!id) return;
    this.manifestLoading.set(true);
    try {
      const m = await firstValueFrom(this.slack.manifest(id));
      // A later pick wins.
      if (this.personaId() === id) this.manifest.set(m);
    } catch (e) {
      this.state.fail(e, 'Could not prepare the Slack app for this persona.');
    } finally {
      this.manifestLoading.set(false);
    }
  }

  async connect(): Promise<void> {
    if (!this.canConnect()) return;
    const ok = await this.state.run(async () => {
      const app = await firstValueFrom(
        this.slack.addApp({ personality_id: this.personaId(), bot_token: this.botToken().trim(), app_token: this.appToken().trim() }),
      );
      this.state.putApp(app);
      this.state.notice.set(
        `${this.personaName()} is in ${app.team_name || 'the workspace'} as @${app.bot_name}. Connect a channel below.`,
      );
    }, 'Could not connect the Slack app.');
    if (ok) {
      this.personaId.set('');
      this.manifest.set(null);
      this.botToken.set('');
      this.appToken.set('');
    }
  }
}

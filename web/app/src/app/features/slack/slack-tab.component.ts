import { ChangeDetectionStrategy, Component, OnInit, inject } from '@angular/core';

import { SlackAppComponent } from './slack-app.component';
import { SlackConnectComponent } from './slack-connect.component';
import { SlackTabState } from './slack-tab.state';

/**
 * Integrations → Slack: give a persona its own Slack app in a workspace (created from a manifest
 * link, connected with its two tokens), and connect channels to relay threads. Talks to
 * /api/slack (internal/slackplugin; ADR 0x032). The parts (connect form, app cards, channels,
 * bindings) share {@link SlackTabState}.
 */
@Component({
  selector: 'app-slack-tab',
  standalone: true,
  imports: [SlackAppComponent, SlackConnectComponent],
  providers: [SlackTabState],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="space-y-6" data-testid="slack-tab">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">Slack</h2>
      <p class="max-w-3xl text-sm text-gray-600 dark:text-gray-400">
        Your persona joins a Slack workspace as its own app, with its own name. Mention it in a connected channel and it answers in a thread
        there; ask it in the app to post a reply and it does. Each connected channel has a relay thread here that keeps the conversation
        together.
      </p>
      <p class="max-w-3xl text-sm text-gray-600 dark:text-gray-400" data-testid="slack-relay-defaults">
        A new relay thread is sandboxed: it reads only itself (none of your memories, other conversations, files or scratchpad), and what
        the persona learns there stays in that thread. The tools that act beyond the conversation start switched off. What the persona
        learns is saved as <strong>External</strong> memories, marked unverified.
      </p>

      @if (state.error(); as message) {
        <div
          class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-800/60 dark:bg-red-900/20 dark:text-red-300"
          role="alert"
          data-testid="slack-error"
        >
          {{ message }}
        </div>
      } @else if (state.notice(); as message) {
        <div
          class="rounded-lg border border-green-200 bg-green-50 p-3 text-sm text-green-800 dark:border-green-800/60 dark:bg-green-900/20 dark:text-green-300"
          role="status"
          data-testid="slack-notice"
        >
          {{ message }}
        </div>
      }

      <app-slack-connect />

      @if (state.loading()) {
        <div class="text-sm text-gray-600 dark:text-gray-400">Loading…</div>
      } @else if (state.apps().length === 0) {
        <div class="text-sm text-gray-600 dark:text-gray-400">No personas in Slack yet.</div>
      }
      @for (a of state.apps(); track a.id) {
        <app-slack-app [app]="a" />
      }
    </div>
  `,
})
export class SlackTabComponent implements OnInit {
  readonly state = inject(SlackTabState);

  ngOnInit(): void {
    void this.state.load();
  }
}

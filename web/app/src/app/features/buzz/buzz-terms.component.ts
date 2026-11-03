import { ChangeDetectionStrategy, Component, computed, input, linkedSignal, output } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { MarkdownModule } from 'ngx-markdown';

import { BuzzJoinPolicy, BuzzTermsAcceptance } from './buzz.service';
import { BUZZ_UI } from './buzz-ui';

/**
 * A community's join terms, shown when claiming an invite needs them accepted for the persona
 * (the API's 409 `terms_required`). The Markdown is rendered with the app's sanitising Markdown
 * setup, as messages are. Accepting emits the terms version to resubmit with.
 */
@Component({
  selector: 'app-buzz-terms',
  standalone: true,
  imports: [FormsModule, MarkdownModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div
      class="mt-3 rounded-lg border border-gray-200 p-3 text-xs dark:border-gray-700"
      role="group"
      aria-label="Join terms"
      data-testid="buzz-terms"
    >
      <p class="font-semibold text-gray-900 dark:text-white">{{ communityName() || 'This community' }} has join terms</p>
      <p class="mt-1 text-gray-600 dark:text-gray-400">
        Read them before {{ personaName() }} joins. Accepting them is on the persona's behalf, and the community records it.
      </p>
      @if (policy().terms_markdown) {
        <div class="mt-2 max-h-64 overflow-y-auto rounded border border-gray-100 p-2 dark:border-gray-700/60">
          <markdown
            class="prose prose-sm max-w-none dark:prose-invert"
            [data]="policy().terms_markdown"
            data-testid="buzz-terms-text"
          ></markdown>
        </div>
      }
      @if (policy().privacy_markdown) {
        <details class="mt-2">
          <summary class="cursor-pointer select-none text-gray-700 dark:text-gray-300">Privacy notice</summary>
          <div class="mt-2 max-h-64 overflow-y-auto rounded border border-gray-100 p-2 dark:border-gray-700/60">
            <markdown class="prose prose-sm max-w-none dark:prose-invert" [data]="policy().privacy_markdown"></markdown>
          </div>
        </details>
      }
      <label class="mt-3 flex items-start gap-2 text-gray-800 dark:text-gray-200">
        <input
          type="checkbox"
          class="mt-0.5"
          [ngModel]="accepted()"
          (ngModelChange)="accepted.set($event)"
          data-testid="buzz-terms-accept"
        />
        <span>I accept these terms on behalf of {{ personaName() }}</span>
      </label>
      @if (policy().age_attestation_required) {
        <label class="mt-2 flex items-start gap-2 text-gray-800 dark:text-gray-200">
          <input
            type="checkbox"
            class="mt-0.5"
            [ngModel]="ageConfirmed()"
            (ngModelChange)="ageConfirmed.set($event)"
            data-testid="buzz-terms-age"
          />
          <span>I confirm I meet this community's age requirement</span>
        </label>
      }
      <div class="mt-3 flex gap-2">
        <button
          type="button"
          [class]="ui.primary"
          [disabled]="!canAccept() || busy()"
          (click)="accept.emit({ accept_terms_version: policy().version, confirm_age: ageConfirmed() })"
          data-testid="buzz-terms-submit"
        >
          Accept and join
        </button>
        <button type="button" [class]="ui.secondary" [disabled]="busy()" (click)="cancel.emit()">Cancel</button>
      </div>
    </div>
  `,
})
export class BuzzTermsComponent {
  readonly policy = input.required<BuzzJoinPolicy>();
  readonly personaName = input('the persona');
  readonly communityName = input<string | null>(null);
  readonly busy = input(false);
  /** The accepted terms version, and whether the owner confirmed the age requirement. */
  readonly accept = output<BuzzTermsAcceptance>();
  readonly cancel = output<void>();

  readonly ui = BUZZ_UI;
  /**
   * Both ticks belong to the policy they were given for. When the terms change between showing and
   * accepting, the server answers 409 with the new policy and the parent passes it to this same
   * instance, so they reset with every policy it is given rather than carrying over to unread text.
   */
  readonly accepted = linkedSignal({ source: this.policy, computation: () => false });
  readonly ageConfirmed = linkedSignal({ source: this.policy, computation: () => false });
  readonly canAccept = computed(() => this.accepted() && (!this.policy().age_attestation_required || this.ageConfirmed()));
}

import { ChangeDetectionStrategy, Component, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { AddBuzzCommunityRequest, BuzzAgent, BuzzJoinPolicy, BuzzService, BuzzTermsAcceptance } from './buzz.service';
import { BuzzAttestationHelpComponent } from './buzz-attestation-help.component';
import { BuzzTabState } from './buzz-tab.state';
import { BuzzTermsComponent } from './buzz-terms.component';
import { BUZZ_UI } from './buzz-ui';

/**
 * "Add a community" for one persona: the community's address or an invite link, an optional
 * invite, and (under Advanced) an optional owner attestation. When the invite's community has
 * join terms, the terms are shown and the request is sent again once they are accepted.
 */
@Component({
  selector: 'app-buzz-add-community',
  standalone: true,
  imports: [FormsModule, BuzzTermsComponent, BuzzAttestationHelpComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (!open()) {
      <button
        type="button"
        class="text-sm font-medium text-indigo-600 hover:underline dark:text-indigo-400"
        (click)="open.set(true)"
        data-testid="buzz-add-community-open"
      >
        + Add a community
      </button>
    } @else {
      <div class="max-w-2xl space-y-3" data-testid="buzz-add-community">
        <label class="block text-xs text-gray-600 dark:text-gray-400">
          Community address or invite link
          <input
            type="url"
            autocomplete="off"
            class="mt-1"
            [class]="ui.input"
            placeholder="https://acme.communities.buzz.xyz"
            [ngModel]="address()"
            (ngModelChange)="address.set($event)"
            [disabled]="!!terms()"
            data-testid="buzz-community-address"
          />
        </label>
        <label class="block text-xs text-gray-600 dark:text-gray-400">
          Invite (optional, when the address is not already an invite link)
          <input
            type="text"
            autocomplete="off"
            class="mt-1"
            [class]="ui.input"
            placeholder="Invite link or code"
            [ngModel]="invite()"
            (ngModelChange)="invite.set($event)"
            [disabled]="!!terms()"
            data-testid="buzz-community-invite"
          />
        </label>
        <details class="text-xs text-gray-700 dark:text-gray-300" [open]="!!attestation()">
          <summary class="cursor-pointer select-none">Advanced</summary>
          <label class="mt-2 block text-gray-600 dark:text-gray-400">
            Owner attestation (optional)
            <textarea
              rows="3"
              class="mt-1 font-mono"
              [class]="ui.input"
              placeholder='["auth", "&lt;your pubkey&gt;", "&lt;conditions&gt;", "&lt;signature&gt;"]'
              [ngModel]="attestation()"
              (ngModelChange)="attestation.set($event)"
              [disabled]="!!terms()"
              data-testid="buzz-community-attestation"
            ></textarea>
          </label>
          <app-buzz-attestation-help [npub]="agent().npub" />
        </details>
        @if (terms(); as policy) {
          <app-buzz-terms
            [policy]="policy"
            [personaName]="agent().persona_name || 'the persona'"
            [busy]="state.busy()"
            (accept)="submit($event)"
            (cancel)="terms.set(null)"
          />
        } @else {
          <div class="flex gap-2">
            <button
              type="button"
              [class]="ui.primary"
              [disabled]="!address().trim() || state.busy()"
              (click)="submit()"
              data-testid="buzz-add-community-submit"
            >
              {{ state.busy() ? 'Joining…' : 'Join' }}
            </button>
            <button type="button" [class]="ui.secondary" [disabled]="state.busy()" (click)="reset()">Cancel</button>
          </div>
        }
      </div>
    }
  `,
})
export class BuzzAddCommunityComponent {
  private readonly buzz = inject(BuzzService);
  readonly state = inject(BuzzTabState);

  readonly agent = input.required<BuzzAgent>();

  readonly ui = BUZZ_UI;
  readonly open = signal(false);
  readonly address = signal('');
  readonly invite = signal('');
  readonly attestation = signal('');
  /** The community's join terms, while they wait to be accepted. */
  readonly terms = signal<BuzzJoinPolicy | null>(null);

  /** Joins; `accepted` is what the owner just agreed to, when resubmitting after the terms. */
  async submit(accepted?: BuzzTermsAcceptance): Promise<void> {
    const body: AddBuzzCommunityRequest = { relay_url: this.address().trim() };
    if (this.invite().trim()) body.invite = this.invite().trim();
    if (this.attestation().trim()) body.attestation = this.attestation().trim();
    if (accepted) Object.assign(body, accepted);
    const res = await this.state.runClaim(
      async () => this.state.putCommunity(await firstValueFrom(this.buzz.addCommunity(this.agent().id, body))),
      'Could not add the community.',
    );
    this.terms.set(res.terms);
    if (res.ok) this.reset();
  }

  reset(): void {
    this.open.set(false);
    this.address.set('');
    this.invite.set('');
    this.attestation.set('');
    this.terms.set(null);
  }
}

import { ChangeDetectionStrategy, Component, computed, inject, input, signal } from '@angular/core';
import { DatePipe } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { BuzzAgent, BuzzCommunity, BuzzJoinPolicy, BuzzService, BuzzTermsAcceptance, UpdateBuzzCommunityRequest } from './buzz.service';
import { BuzzAttestationHelpComponent } from './buzz-attestation-help.component';
import { BuzzBindingComponent } from './buzz-binding.component';
import { BuzzChannelsComponent } from './buzz-channels.component';
import { BuzzTabState } from './buzz-tab.state';
import { BuzzTermsComponent } from './buzz-terms.component';
import { BUZZ_UI, buzzStatusClass, buzzStatusLabel } from './buzz-ui';
import { shortNpub } from './npub';

/** Which inline editor is open on a community. */
type Editor = 'invite' | 'attestation' | null;

/**
 * One community a persona is in (or trying to get into): its access status, the actions that
 * fix access (check again, paste an invite, give an attestation), pause and remove, and, once
 * the persona is let in, its connected channels and the channel list to connect more.
 */
@Component({
  selector: 'app-buzz-community',
  standalone: true,
  imports: [DatePipe, FormsModule, BuzzAttestationHelpComponent, BuzzBindingComponent, BuzzChannelsComponent, BuzzTermsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let c = community();
    <div class="border-t border-gray-200 dark:border-gray-700" data-testid="buzz-community">
      <div class="flex flex-wrap items-start justify-between gap-3 p-4">
        <div class="min-w-0">
          <h4 class="text-sm font-semibold text-gray-900 dark:text-white">{{ c.name }}</h4>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-500 dark:text-gray-400">
            <span [class]="ui.chip + ' ' + statusClass(c.status)" data-testid="buzz-community-status">{{ statusLabel(c.status) }}</span>
            <span class="break-all">{{ c.relay_url }}</span>
            @if (c.attested) {
              <span [attr.title]="c.owner_pubkey || null">Attested{{ c.owner_pubkey ? ' by ' + short(c.owner_pubkey) : '' }}</span>
            }
            @if (c.last_connected_at) {
              <span>Last connected {{ c.last_connected_at | date: 'short' }}</span>
            }
          </div>
          @if (c.last_error && c.status !== 'active') {
            <p [class]="ui.error" data-testid="buzz-community-error">{{ c.last_error }}</p>
          }
        </div>
        <div class="flex flex-wrap gap-2">
          @if (c.status !== 'disabled') {
            <button
              type="button"
              [class]="ui.secondary"
              [disabled]="state.busy()"
              (click)="update({ recheck: true })"
              data-testid="buzz-community-recheck"
            >
              Check again
            </button>
          }
          <button
            type="button"
            [class]="ui.secondary"
            [disabled]="state.busy()"
            (click)="update({ enabled: c.status === 'disabled' })"
            data-testid="buzz-community-pause"
          >
            {{ c.status === 'disabled' ? 'Resume' : 'Pause' }}
          </button>
          <button
            type="button"
            [class]="ui.secondary"
            [disabled]="state.busy()"
            (click)="edit('invite')"
            data-testid="buzz-community-invite-open"
          >
            Paste an invite
          </button>
          <button
            type="button"
            [class]="ui.secondary"
            [disabled]="state.busy()"
            (click)="edit('attestation')"
            data-testid="buzz-community-attestation-open"
          >
            {{ c.attested ? 'Change attestation' : 'Add attestation' }}
          </button>
          <button type="button" [class]="ui.danger" [disabled]="state.busy()" (click)="remove()" data-testid="buzz-community-remove">
            Remove
          </button>
        </div>
      </div>

      @if (editor() === 'invite') {
        <div class="max-w-2xl px-4 pb-4" data-testid="buzz-community-invite-form">
          <label class="block text-xs text-gray-600 dark:text-gray-400">
            Invite link or code
            <input
              type="text"
              autocomplete="off"
              class="mt-1"
              [class]="ui.input"
              [ngModel]="invite()"
              (ngModelChange)="invite.set($event)"
              [disabled]="!!terms()"
              data-testid="buzz-community-invite-input"
            />
          </label>
          @if (terms(); as policy) {
            <app-buzz-terms
              [policy]="policy"
              [personaName]="agent().persona_name || 'the persona'"
              [communityName]="c.name"
              [busy]="state.busy()"
              (accept)="claimInvite($event)"
              (cancel)="terms.set(null)"
            />
          } @else {
            <div class="mt-2 flex gap-2">
              <button
                type="button"
                [class]="ui.primary"
                [disabled]="!invite().trim() || state.busy()"
                (click)="claimInvite()"
                data-testid="buzz-community-invite-submit"
              >
                Use invite
              </button>
              <button type="button" [class]="ui.secondary" (click)="edit(null)">Cancel</button>
            </div>
          }
        </div>
      } @else if (editor() === 'attestation') {
        <div class="max-w-2xl px-4 pb-4" data-testid="buzz-community-attestation-form">
          <label class="block text-xs text-gray-600 dark:text-gray-400">
            Owner attestation
            <textarea
              rows="3"
              class="mt-1 font-mono"
              [class]="ui.input"
              [ngModel]="attestation()"
              (ngModelChange)="attestation.set($event)"
              data-testid="buzz-community-attestation-input"
            ></textarea>
          </label>
          <app-buzz-attestation-help [npub]="agent().npub" />
          <div class="mt-2 flex flex-wrap gap-2">
            <button
              type="button"
              [class]="ui.primary"
              [disabled]="!attestation().trim() || state.busy()"
              (click)="saveAttestation(attestation())"
              data-testid="buzz-community-attestation-save"
            >
              Save attestation
            </button>
            @if (c.attested) {
              <button
                type="button"
                [class]="ui.secondary"
                [disabled]="state.busy()"
                (click)="saveAttestation('')"
                data-testid="buzz-community-attestation-remove"
              >
                Remove attestation
              </button>
            }
            <button type="button" [class]="ui.secondary" (click)="edit(null)">Cancel</button>
          </div>
        </div>
      }

      @for (b of bindings(); track b.id) {
        <app-buzz-binding [binding]="b" [communityName]="c.name" />
      }
      @if (c.status === 'active') {
        <div class="p-4">
          <app-buzz-channels [community]="c" [personalityId]="agent().personality_id" />
        </div>
      } @else if (c.status === 'needs_access') {
        <p class="px-4 pb-4" [class]="ui.hint">
          Once the community lets the persona in, its channels show here. Share the persona's npub with an admin, paste an invite, or add
          your attestation, then choose Check again.
        </p>
      }
    </div>
  `,
})
export class BuzzCommunityComponent {
  private readonly buzz = inject(BuzzService);
  private readonly confirm = inject(ConfirmationService);
  readonly state = inject(BuzzTabState);

  readonly community = input.required<BuzzCommunity>();
  readonly agent = input.required<BuzzAgent>();

  readonly ui = BUZZ_UI;
  readonly statusClass = buzzStatusClass;
  readonly statusLabel = buzzStatusLabel;
  readonly short = shortNpub;
  readonly editor = signal<Editor>(null);
  readonly invite = signal('');
  readonly attestation = signal('');
  readonly terms = signal<BuzzJoinPolicy | null>(null);

  readonly bindings = computed(() => this.state.bindingsFor(this.community().id));

  edit(editor: Editor): void {
    this.editor.set(this.editor() === editor ? null : editor);
    this.invite.set('');
    this.attestation.set('');
    this.terms.set(null);
  }

  async update(body: UpdateBuzzCommunityRequest): Promise<boolean> {
    return this.state.run(
      async () => this.state.putCommunity(await firstValueFrom(this.buzz.updateCommunity(this.community().id, body))),
      'Could not update the community.',
    );
  }

  /** Claims the pasted invite; when the community has join terms, shows them and claims again once accepted. */
  async claimInvite(accepted?: BuzzTermsAcceptance): Promise<void> {
    const body: UpdateBuzzCommunityRequest = { invite: this.invite().trim() };
    if (accepted) Object.assign(body, accepted);
    const res = await this.state.runClaim(
      async () => this.state.putCommunity(await firstValueFrom(this.buzz.updateCommunity(this.community().id, body))),
      'Could not use that invite.',
    );
    this.terms.set(res.terms);
    if (res.ok) this.edit(null);
  }

  /** Saves the attestation, or removes it with "". */
  async saveAttestation(value: string): Promise<void> {
    if (await this.update({ attestation: value.trim() })) this.edit(null);
  }

  async remove(): Promise<void> {
    const ok = await this.confirm.confirm({
      title: 'Remove community',
      message: `Take ${this.agent().persona_name || 'the persona'} out of ${this.community().name}? Its channels are disconnected; the relay threads stay in your thread list. Nothing is deleted in Buzz.`,
      confirmText: 'Remove',
      type: 'danger',
    });
    if (!ok) return;
    await this.state.run(async () => {
      await firstValueFrom(this.buzz.removeCommunity(this.community().id));
      this.state.removeCommunity(this.community());
    }, 'Could not remove the community.');
  }
}

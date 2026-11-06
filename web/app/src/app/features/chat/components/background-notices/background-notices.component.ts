import { ChangeDetectionStrategy, Component, effect, inject, input, signal, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { BackgroundNotice, UtilityRole } from '../../../../core/models/utility-model.model';
import { BackgroundNoticesService } from '../../../../core/services/background-notices.service';
import { ProviderKeysService } from '../../../../core/services/provider-keys.service';
import { UTILITY_ROLE_COPY } from '../../../../core/utils/utility-role-copy';
import { ProfileSettingsModalService } from '../../../profile-settings/profile-settings-modal.service';

/** The fix a notice offers, if the person can make one. */
type NoticeAction = 'add-key' | 'background-models' | null;

interface NoticeView {
  id: string;
  /** What didn't happen. */
  what: string;
  /** Why, and what happens next. */
  why: string;
  action: NoticeAction;
  actionLabel: string;
}

/** "a" or "an" before a word, by its first letter. */
function article(word: string): string {
  return /^[aeiou]/i.test(word) ? 'an' : 'a';
}

const STEP_COPY: Record<string, string> = {
  memory: "Memories from this chat weren't saved.",
  scratchpad: "The scratchpad wasn't updated from this chat.",
  summary: "This chat's summary wasn't updated.",
};

/**
 * Background work on this chat that failed or fell back (ADR 0x027 section 6), with the fix
 * when there is one. A notice clears itself when the same step succeeds later, so fixing the
 * cause is all the person has to do; they can also dismiss it.
 */
@Component({
  selector: 'app-background-notices',
  standalone: true,
  templateUrl: './background-notices.component.html',
  styleUrl: './background-notices.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class BackgroundNoticesComponent {
  private readonly service = inject(BackgroundNoticesService);
  private readonly keys = inject(ProviderKeysService);
  private readonly settings = inject(ProfileSettingsModalService);

  readonly chatId = input<string | null>(null);
  /** Changes whenever a turn on the chat fully ends, when a checkpoint may have run. */
  readonly settled = input(0);

  readonly notices = signal<NoticeView[]>([]);
  /** The chat the shown notices belong to; another chat's are never shown. */
  private shownFor: string | null = null;
  /** Only the latest load may land, so an earlier, slower one can't overwrite it. */
  private loadToken = 0;

  constructor() {
    effect(() => {
      const chatId = this.chatId();
      this.settled();
      untracked(() => void this.load(chatId));
    });
  }

  async load(chatId: string | null): Promise<void> {
    const token = ++this.loadToken;
    if (chatId !== this.shownFor) {
      this.notices.set([]);
      this.shownFor = chatId;
    }
    if (!chatId) return;
    try {
      const notices = await firstValueFrom(this.service.list(chatId));
      if (token === this.loadToken) {
        this.notices.set(notices.map(n => this.view(n)));
      }
    } catch (error) {
      // A notice is a courtesy, so a failed load shows nothing rather than an error of its
      // own; it is logged so the failure itself doesn't go unseen.
      console.warn('Failed to load background notices:', error);
    }
  }

  async dismiss(id: string): Promise<void> {
    const chatId = this.chatId();
    if (!chatId) return;
    this.notices.update(list => list.filter(n => n.id !== id));
    try {
      await firstValueFrom(this.service.dismiss(chatId, id));
    } catch {
      await this.load(chatId);
    }
  }

  fix(action: NoticeAction): void {
    if (action === 'add-key') {
      this.settings.open('providers');
    } else if (action === 'background-models') {
      this.settings.open('profile');
    }
  }

  private view(n: BackgroundNotice): NoticeView {
    const view = this.describe(n);
    // Where the deployment supplies the keys, it also chooses the models: nothing here is the
    // person's to fix, so the notice only informs.
    return this.keys.accountsSupplyKeys() ? view : { ...view, action: null, actionLabel: '' };
  }

  private describe(n: BackgroundNotice): NoticeView {
    const times = n.count > 1 ? ` This has happened ${n.count} times.` : '';
    if (n.step.startsWith('fallback_')) {
      const role = n.step.slice('fallback_'.length) as UtilityRole;
      // A role this app doesn't know yet (a newer server's) gets a general sentence rather than
      // its raw name.
      const label = UTILITY_ROLE_COPY[role]?.label;
      return {
        id: n.id,
        what: label
          ? `The model chosen for ${label} wasn't available, so another was used.`
          : "A model chosen for background work wasn't available, so another was used.",
        why: `It may have been turned off, or its provider removed.${times}`,
        action: 'background-models',
        actionLabel: 'Change background models',
      };
    }
    const what = STEP_COPY[n.step] ?? "Some background work on this chat didn't finish.";
    const provider = n.provider || 'The provider';
    if (n.code === 'provider_not_configured') {
      if (!this.keys.accountsSupplyKeys()) {
        return {
          id: n.id,
          what,
          why: `${provider} isn't set up on this server${n.model ? ` for ${n.model}` : ''}.${times}`,
          action: null,
          actionLabel: '',
        };
      }
      return {
        id: n.id,
        what,
        why: `There's no ${provider} key${n.model ? ` for ${n.model}` : ''}. Add one, or choose another background model.${times}`,
        action: 'add-key',
        actionLabel: `Add ${article(provider)} ${provider} key`,
      };
    }
    return {
      id: n.id,
      what,
      why: `${provider} returned an error${n.model ? ` (${n.model})` : ''}. It runs again as the chat goes on, and this goes away once it works.${times}`,
      action: 'background-models',
      actionLabel: 'Change background models',
    };
  }
}

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

  constructor() {
    effect(() => {
      const chatId = this.chatId();
      this.settled();
      untracked(() => void this.load(chatId));
    });
  }

  async load(chatId: string | null): Promise<void> {
    if (!chatId) {
      this.notices.set([]);
      return;
    }
    try {
      const notices = await firstValueFrom(this.service.list(chatId));
      if (this.chatId() === chatId) {
        this.notices.set(notices.map(n => this.view(n)));
      }
    } catch {
      // A notice is a courtesy; failing to load one says nothing new.
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
    const times = n.count > 1 ? ` This has happened ${n.count} times.` : '';
    if (n.step.startsWith('fallback_')) {
      const role = n.step.slice('fallback_'.length) as UtilityRole;
      const label = UTILITY_ROLE_COPY[role]?.label ?? role;
      return {
        id: n.id,
        what: `The model chosen for ${label} wasn't available, so another was used.`,
        why: `It may have been turned off, or its provider removed.${times}`,
        action: 'background-models',
        actionLabel: 'Change background models',
      };
    }
    const what = STEP_COPY[n.step] ?? "Some background work on this chat didn't finish.";
    const provider = n.provider || 'The provider';
    if (n.code === 'provider_not_configured') {
      const canAdd = this.keys.accountsSupplyKeys();
      return {
        id: n.id,
        what,
        why: `There's no ${provider} key${n.model ? ` for ${n.model}` : ''}. Add one, or choose another background model.${times}`,
        action: canAdd ? 'add-key' : 'background-models',
        actionLabel: canAdd ? `Add ${article(provider)} ${provider} key` : 'Change background models',
      };
    }
    return {
      id: n.id,
      what,
      why: `${provider} returned an error${n.model ? ` (${n.model})` : ''}. It's tried again after a few more messages.${times}`,
      action: 'background-models',
      actionLabel: 'Change background models',
    };
  }
}

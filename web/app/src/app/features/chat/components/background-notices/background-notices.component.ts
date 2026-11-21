import { ChangeDetectionStrategy, Component, computed, effect, inject, input, signal, untracked } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { BackgroundNotice, UtilityRole } from '../../../../core/models/utility-model.model';
import { BackgroundNoticesService } from '../../../../core/services/background-notices.service';
import { ProviderKeysService } from '../../../../core/services/provider-keys.service';
import { UTILITY_ROLE_COPY } from '../../../../core/utils/utility-role-copy';
import { ProfileSettingsModalService } from '../../../profile-settings/profile-settings-modal.service';

/** The fix a notice offers, if the person can make one. */
type NoticeAction = 'add-key' | 'background-models' | null;

interface NoticeView {
  /** The notices shown as this one: a role's, with one cause. */
  ids: string[];
  /** Tracks the view across reloads. */
  key: string;
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

/** What didn't happen, for a step shown on its own. */
const STEP_COPY: Record<string, string> = {
  memory: "Memories from this chat weren't saved.",
  scratchpad: "The scratchpad wasn't updated from this chat.",
  summary: "This chat's summary wasn't updated.",
  memory_query: "Replies in this chat aren't drawing on memories.",
  chat_name: "This chat wasn't named.",
  mood_select: "The mood didn't change on its own.",
  expression_pick: "The expression didn't change with the reply.",
};

/** A step's work in a few words, for a role's notices shown together. */
const STEP_SHORT: Record<string, string> = {
  memory: 'saving memories',
  scratchpad: 'updating the scratchpad',
  summary: 'summarizing',
  memory_query: 'finding memories for replies',
  chat_name: 'naming the chat',
  mood_select: 'choosing a mood',
  expression_pick: 'picking expressions',
};

/** Notices with one role and one cause read as one, so a broken model is one notice, not four. */
function groupKey(n: BackgroundNotice): string {
  const fallback = n.step.startsWith('fallback_');
  return [fallback ? n.step : (n.role ?? n.step), n.code, n.provider ?? '', n.model ?? ''].join('|');
}

/** "a, b and c". */
function listed(items: string[]): string {
  return items.length < 2 ? items.join('') : `${items.slice(0, -1).join(', ')} and ${items[items.length - 1]}`;
}

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

  /** The chat's notices as the server sent them, less any dismissed here since. */
  private readonly raw = signal<BackgroundNotice[]>([]);
  /**
   * What each notice says, a role's notices with one cause together. Derived rather than worked
   * out once on load, so the words follow whether accounts supply keys, which may still be
   * loading on a direct visit to a chat.
   */
  readonly notices = computed(() => {
    const groups = new Map<string, BackgroundNotice[]>();
    for (const n of this.raw()) {
      const key = groupKey(n);
      groups.set(key, [...(groups.get(key) ?? []), n]);
    }
    return [...groups.entries()].map(([key, group]) => this.view(key, group));
  });
  /** The chat the shown notices belong to; another chat's are never shown. */
  private shownFor: string | null = null;
  /**
   * Notices dismissed on the shown chat. A list request already on its way when one was
   * dismissed would otherwise bring it back.
   */
  private dismissed = new Set<string>();
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
      this.raw.set([]);
      this.shownFor = chatId;
      this.dismissed = new Set();
    }
    if (!chatId) return;
    try {
      const notices = await firstValueFrom(this.service.list(chatId));
      if (token === this.loadToken) {
        this.raw.set(notices.filter(n => !this.dismissed.has(n.id)));
      }
    } catch (error) {
      // A notice is a courtesy, so a failed load shows nothing rather than an error of its
      // own; it is logged so the failure itself doesn't go unseen.
      console.warn('Failed to load background notices:', error);
    }
  }

  /** Dismisses every notice shown as one. */
  async dismiss(ids: string[]): Promise<void> {
    const chatId = this.chatId();
    if (!chatId) return;
    ids.forEach(id => this.dismissed.add(id));
    this.raw.update(list => list.filter(n => !ids.includes(n.id)));
    const results = await Promise.allSettled(ids.map(id => firstValueFrom(this.service.dismiss(chatId, id))));
    const failed = results.filter(r => r.status === 'rejected');
    if (failed.length === 0) return;
    // Logged like a failed load, so the failure itself doesn't go unseen.
    console.warn('Failed to dismiss a background notice:', (failed[0] as PromiseRejectedResult).reason);
    // Show it again, but only on its own chat: the person may have moved to another meanwhile.
    if (chatId === this.chatId()) {
      ids.forEach(id => this.dismissed.delete(id));
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

  private view(key: string, group: BackgroundNotice[]): NoticeView {
    const view = { ...this.describe(group), ids: group.map(n => n.id), key };
    // Where the deployment supplies the keys, it also chooses the models: nothing here is the
    // person's to fix, so the notice only informs.
    return this.keys.accountsSupplyKeys() ? view : { ...view, action: null, actionLabel: '' };
  }

  private describe(group: BackgroundNotice[]): Omit<NoticeView, 'ids' | 'key'> {
    const n = group[0];
    const count = group.reduce((most, g) => Math.max(most, g.count), 0);
    const times = count > 1 ? ` This has happened ${count} times.` : '';
    // A role this app doesn't know yet (a newer server's) gets a general sentence rather than
    // its raw name.
    const roleLabel = (role: string | undefined) => (role ? UTILITY_ROLE_COPY[role as UtilityRole]?.label : undefined);
    if (n.step.startsWith('fallback_')) {
      const label = roleLabel(n.step.slice('fallback_'.length));
      return {
        what: label ? `The model chosen for ${label} wasn't available.` : "A model chosen for background work wasn't available.",
        why: `It may have been turned off, or its provider removed.${this.keys.accountsSupplyKeys() ? ' Choose another.' : ''}${times}`,
        action: 'background-models',
        actionLabel: 'Change background models',
      };
    }
    const label = roleLabel(n.role);
    const shorts = group.map(g => STEP_SHORT[g.step]).filter((s): s is string => !!s);
    const what =
      group.length > 1 && label && shorts.length === group.length
        ? `${label} didn't run on this chat: ${listed(shorts)}.`
        : (STEP_COPY[n.step] ?? "Some background work on this chat didn't finish.");
    if (n.code === 'model_not_set') {
      if (!this.keys.accountsSupplyKeys()) {
        return { what, why: `This server hasn't chosen a model for it.${times}`, action: null, actionLabel: '' };
      }
      return {
        what,
        why: `No model is chosen for ${label ?? 'this background work'}.${times}`,
        action: 'background-models',
        actionLabel: 'Choose background models',
      };
    }
    if (n.code === 'model_unavailable') {
      return {
        what,
        why: `The model chosen for ${label ?? 'this background work'} can't be used any more: it may have been turned off, or its provider removed.${times}`,
        action: 'background-models',
        actionLabel: 'Choose background models',
      };
    }
    const provider = n.provider || 'The provider';
    if (n.code === 'provider_not_configured') {
      if (!this.keys.accountsSupplyKeys()) {
        return {
          what,
          why: `${provider} isn't set up on this server${n.model ? ` for ${n.model}` : ''}.${times}`,
          action: null,
          actionLabel: '',
        };
      }
      return {
        what,
        why: `There's no ${provider} key${n.model ? ` for ${n.model}` : ''}. Add one, or choose another background model.${times}`,
        action: 'add-key',
        actionLabel: `Add ${article(provider)} ${provider} key`,
      };
    }
    return {
      what,
      why: `${provider} returned an error${n.model ? ` (${n.model})` : ''}. It runs again as the chat goes on, and this goes away once it works.${times}`,
      action: 'background-models',
      actionLabel: 'Change background models',
    };
  }
}

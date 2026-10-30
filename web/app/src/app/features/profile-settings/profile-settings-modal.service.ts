import { Injectable, signal } from '@angular/core';

import type { ExtensionProfileSettingsTab } from '../../extensions/profile-settings-tabs';

/** Base tabs plus any a build adds through `extensions/profile-settings-tabs.ts`. */
export type ProfileSettingsTab = 'profile' | 'providers' | 'subscription' | 'usage' | 'billing' | ExtensionProfileSettingsTab;

const BILLING_RELATED_TABS = new Set<ProfileSettingsTab>(['subscription', 'usage', 'billing']);

@Injectable({
  providedIn: 'root',
})
export class ProfileSettingsModalService {
  readonly openState = signal(false);
  readonly activeTab = signal<ProfileSettingsTab>('profile');

  /**
   * Increments when the modal closes from subscription, usage, or billing so listeners
   * can react via `effect()` without manual RxJS subscription cleanup.
   */
  readonly billingTabClosedRevision = signal(0);

  /**
   * A request to open the model chooser on the Providers tab: a connection id,
   * or a preset id (such as 'openai') for the account's connection made from it,
   * or '' for its first connection. The Providers tab takes it once it has
   * loaded its connections, and clears it.
   */
  readonly modelChooserRequest = signal<string | null>(null);

  open(tab: ProfileSettingsTab = 'profile'): void {
    this.activeTab.set(tab);
    this.openState.set(true);
  }

  /** Opens settings on the Providers tab with a connection's model chooser open (see modelChooserRequest). */
  openModelChooser(connectionOrPreset = ''): void {
    this.modelChooserRequest.set(connectionOrPreset);
    this.open('providers');
  }

  close(): void {
    if (!this.openState()) {
      return;
    }
    const tab = this.activeTab();
    this.openState.set(false);
    if (BILLING_RELATED_TABS.has(tab)) {
      this.billingTabClosedRevision.update(revision => revision + 1);
    }
  }

  setTab(tab: ProfileSettingsTab): void {
    this.activeTab.set(tab);
  }
}

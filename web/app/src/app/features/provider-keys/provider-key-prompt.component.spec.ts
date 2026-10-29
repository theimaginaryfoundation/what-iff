import { computed, provideZonelessChangeDetection, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { ProviderAccess } from '../../core/models/provider-key.model';
import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { ProfileSettingsModalService } from '../profile-settings/profile-settings-modal.service';
import { ProviderKeyPromptComponent } from './provider-key-prompt.component';

describe('ProviderKeyPromptComponent', () => {
  let access: WritableSignal<ProviderAccess | null>;
  let keys: { needsFirstKey: ReturnType<typeof computed<boolean>>; refreshAccess: ReturnType<typeof vi.fn> };
  let settings: ProfileSettingsModalService;

  beforeEach(async () => {
    access = signal<ProviderAccess | null>(null);
    keys = {
      needsFirstKey: computed(() => access()?.keys_from === 'account' && access()!.providers.length === 0),
      refreshAccess: vi.fn().mockReturnValue(of(null)),
    };
    await TestBed.configureTestingModule({
      imports: [ProviderKeyPromptComponent],
      providers: [provideZonelessChangeDetection(), ProfileSettingsModalService, { provide: ProviderKeysService, useValue: keys }],
    }).compileComponents();
    settings = TestBed.inject(ProfileSettingsModalService);
  });

  function render() {
    const fixture = TestBed.createComponent(ProviderKeyPromptComponent);
    fixture.detectChanges();
    return fixture;
  }

  it('loads access when the app starts', () => {
    render();
    expect(keys.refreshAccess).toHaveBeenCalled();
  });

  it('asks for a key when the account needs its first one', () => {
    access.set({ keys_from: 'account', providers: [], custom_addresses: true });
    expect(render().componentInstance.open()).toBe(true);
  });

  it('stays away once the account has a key, or where the deployment supplies them', () => {
    const fixture = render();
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    expect(fixture.componentInstance.open()).toBe(false);
    access.set({ keys_from: 'deployment', providers: [], custom_addresses: true });
    expect(fixture.componentInstance.open()).toBe(false);
  });

  // It steps aside while settings are open, and returns if they close without a key.
  it('opens settings on the API keys tab and steps aside while they are open', () => {
    access.set({ keys_from: 'account', providers: [], custom_addresses: true });
    const fixture = render();

    fixture.componentInstance.addKey();
    expect(settings.openState()).toBe(true);
    expect(settings.activeTab()).toBe('providers');
    expect(fixture.componentInstance.open()).toBe(false);

    settings.close();
    expect(fixture.componentInstance.open()).toBe(true);
  });
});

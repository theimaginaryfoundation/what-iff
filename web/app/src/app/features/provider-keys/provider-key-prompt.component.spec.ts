import { computed, provideZonelessChangeDetection, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { BehaviorSubject, of } from 'rxjs';

import { ProviderAccess } from '../../core/models/provider-key.model';
import { UserPreferences } from '../../core/models/user.model';
import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { NIL_UUID } from '../../core/utils/default-model';
import { ProfileSettingsModalService } from '../profile-settings/profile-settings-modal.service';
import { ProviderKeyPromptComponent } from './provider-key-prompt.component';

describe('ProviderKeyPromptComponent', () => {
  let access: WritableSignal<ProviderAccess | null>;
  let keys: {
    access: WritableSignal<ProviderAccess | null>;
    needsFirstKey: ReturnType<typeof computed<boolean>>;
    refreshAccess: ReturnType<typeof vi.fn>;
  };
  let prefs: BehaviorSubject<Pick<UserPreferences, 'default_model_id'> | null>;
  let preferences: { preferences$: BehaviorSubject<Pick<UserPreferences, 'default_model_id'> | null>; getUserPreferences: ReturnType<typeof vi.fn> };
  let settings: ProfileSettingsModalService;

  beforeEach(async () => {
    access = signal<ProviderAccess | null>(null);
    keys = {
      access,
      needsFirstKey: computed(() => access()?.keys_from === 'account' && access()!.providers.length === 0),
      refreshAccess: vi.fn().mockReturnValue(of(null)),
    };
    prefs = new BehaviorSubject<Pick<UserPreferences, 'default_model_id'> | null>(null);
    preferences = { preferences$: prefs, getUserPreferences: vi.fn().mockReturnValue(of(null)) };
    await TestBed.configureTestingModule({
      imports: [ProviderKeyPromptComponent],
      providers: [
        provideZonelessChangeDetection(),
        ProfileSettingsModalService,
        { provide: ProviderKeysService, useValue: keys },
        { provide: UserPreferencesService, useValue: preferences },
      ],
    }).compileComponents();
    settings = TestBed.inject(ProfileSettingsModalService);
  });

  function render() {
    const fixture = TestBed.createComponent(ProviderKeyPromptComponent);
    fixture.detectChanges();
    return fixture;
  }

  it('loads access and preferences when the app starts', () => {
    render();
    TestBed.tick();
    expect(keys.refreshAccess).toHaveBeenCalled();
    expect(preferences.getUserPreferences).toHaveBeenCalled();
  });

  it('asks for a key when the account needs its first one', () => {
    access.set({ keys_from: 'account', providers: [], custom_addresses: true });
    expect(render().componentInstance.open()).toBe(true);
  });

  it('stays away once the account has a key, or where the deployment supplies them', () => {
    const fixture = render();
    prefs.next({ default_model_id: 'm-1' });
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    expect(fixture.componentInstance.open()).toBe(false);
    access.set({ keys_from: 'deployment', providers: [], custom_addresses: true });
    expect(fixture.componentInstance.open()).toBe(false);
  });

  // ADR 0x027: on an install without a model catalog, a key alone is not enough to chat.
  it('asks for models once there is a key but no model yet', () => {
    const fixture = render();
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    prefs.next({ default_model_id: NIL_UUID });
    expect(fixture.componentInstance.open()).toBe(true);
    expect(fixture.componentInstance.step()).toBe(1);

    prefs.next({ default_model_id: 'm-1' });
    expect(fixture.componentInstance.open()).toBe(false);
  });

  it('never asks for models where the deployment supplies keys, or before preferences load', () => {
    const fixture = render();
    access.set({ keys_from: 'deployment', providers: ['openai'], custom_addresses: true });
    prefs.next({ default_model_id: NIL_UUID });
    expect(fixture.componentInstance.open()).toBe(false);

    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    prefs.next(null);
    expect(fixture.componentInstance.open()).toBe(false);
  });

  it("opens the chooser on the account's OpenAI connection, and looks again when settings close", () => {
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    prefs.next({ default_model_id: NIL_UUID });
    const fixture = render();
    TestBed.tick();
    preferences.getUserPreferences.mockClear();

    fixture.componentInstance.chooseModels();
    expect(settings.activeTab()).toBe('providers');
    expect(settings.modelChooserRequest()).toBe('openai');
    expect(fixture.componentInstance.open()).toBe(false);

    settings.close();
    TestBed.tick();
    expect(preferences.getUserPreferences).toHaveBeenCalled();
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

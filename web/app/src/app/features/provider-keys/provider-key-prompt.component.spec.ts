import { computed, provideZonelessChangeDetection, signal, WritableSignal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { BehaviorSubject, of } from 'rxjs';

import { ProviderAccess } from '../../core/models/provider-key.model';
import { UserPreferences } from '../../core/models/user.model';
import { UtilityModels } from '../../core/models/utility-model.model';
import { ModelService } from '../../core/services/model.service';
import { PersonalityPresenceService } from '../../core/services/personality-presence.service';
import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { UtilityModelsService } from '../../core/services/utility-models.service';
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
  let utility: { get: ReturnType<typeof vi.fn>; choose: ReturnType<typeof vi.fn>; switchOff: ReturnType<typeof vi.fn> };
  let presence: { hasPersonality: ReturnType<typeof vi.fn> };

  function utilityModels(setRoles: string[] = []): UtilityModels {
    return {
      accounts_choose: true,
      roles: (['memory', 'memory_merge', 'simple', 'personality'] as const).map(role => ({
        role,
        account: setRoles.includes(role) ? { off: false, model_id: 'm-1', reasoning_effort: null, service_tier: null } : null,
        deployment: null,
      })),
    };
  }

  beforeEach(async () => {
    utility = { get: vi.fn().mockReturnValue(of(utilityModels(['memory', 'memory_merge', 'simple', 'personality']))), choose: vi.fn(), switchOff: vi.fn() };
    presence = { hasPersonality: vi.fn().mockReturnValue(of(false)) };
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
        { provide: UtilityModelsService, useValue: utility },
        { provide: PersonalityPresenceService, useValue: presence },
        { provide: ModelService, useValue: { models$: new BehaviorSubject([{ id: 'm-1', name: 'm', display_name: 'Model One', description: '', tool_support: true }]), getModels: vi.fn(() => of([])) } },
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

  // ADR 0x027 section 6: no model is chosen for background work in code, so the account chooses.
  it('asks for background models once the account has a key and a default model, while a role has none', () => {
    utility.get.mockReturnValue(of(utilityModels(['memory'])));
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    prefs.next({ default_model_id: 'm-1' });
    const fixture = render();
    TestBed.tick();
    fixture.detectChanges();
    expect(fixture.componentInstance.open()).toBe(true);
    expect(fixture.componentInstance.step()).toBe(2);
    const el = fixture.nativeElement as HTMLElement;
    expect(el.querySelector('[data-testid="background-models-step"]')).not.toBeNull();
    expect(el.textContent).toContain('Welcome to WhatIff');

    fixture.componentInstance.backgroundModelsSaved(utilityModels(['memory', 'memory_merge', 'simple', 'personality']));
    expect(fixture.componentInstance.open()).toBe(false);
  });

  it('words the background step for an account past onboarding, without the step list', () => {
    utility.get.mockReturnValue(of(utilityModels()));
    presence.hasPersonality.mockReturnValue(of(true));
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    prefs.next({ default_model_id: 'm-1' });
    const fixture = render();
    TestBed.tick();
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    expect(el.textContent).toContain('Something new');
    expect(el.textContent).toContain("Memories and summaries start again once you've chosen.");
    expect(el.querySelector('.key-prompt__steps')).toBeNull();
  });

  it('never asks for background models where the deployment chooses them, or where they can\'t be read', () => {
    access.set({ keys_from: 'account', providers: ['openai'], custom_addresses: true });
    prefs.next({ default_model_id: 'm-1' });
    utility.get.mockReturnValue(of({ ...utilityModels(), accounts_choose: false }));
    const fixture = render();
    TestBed.tick();
    expect(fixture.componentInstance.open()).toBe(false);

    fixture.componentInstance.utility.set(null);
    expect(fixture.componentInstance.open()).toBe(false);
  });
});

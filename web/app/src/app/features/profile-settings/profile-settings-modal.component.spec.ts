import { Signal, WritableSignal, computed, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Router } from '@angular/router';
import { of, throwError } from 'rxjs';

import { ProfileSettingsModalComponent } from './profile-settings-modal.component';
import { ProfileSettingsModalService } from './profile-settings-modal.service';
import { AuthService } from '../../core/services/auth.service';
import { ExternalAuthProvider, NoopExternalAuthProvider } from '../../core/auth/external-auth.provider';
import { LoggerService } from '../../core/services/logger.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ThemeService } from '../../core/services/theme.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { ProviderAccess } from '../../core/models/provider-key.model';

describe('ProfileSettingsModalComponent (open-source profile-only)', () => {
  let preferencesService: {
    getUserPreferences: ReturnType<typeof vi.fn>;
    updateUserPreferences: ReturnType<typeof vi.fn>;
  };
  let personalityService: {
    listPersonalities: ReturnType<typeof vi.fn>;
  };
  let router: { navigate: ReturnType<typeof vi.fn> };
  let authService: {
    getUserProfile: ReturnType<typeof vi.fn>;
    updateProfile: ReturnType<typeof vi.fn>;
    updatePassword: ReturnType<typeof vi.fn>;
    logoutPreferred: ReturnType<typeof vi.fn>;
  };
  let user: Record<string, unknown>;
  let logger: { error: ReturnType<typeof vi.fn> };
  let providerAccess: WritableSignal<ProviderAccess | null>;
  let providerKeys: {
    access: WritableSignal<ProviderAccess | null>;
    accountsSupplyKeys: Signal<boolean>;
    refreshAccess: ReturnType<typeof vi.fn>;
    list: ReturnType<typeof vi.fn>;
  };
  /** Swapped per test before the component is created; the provider factory reads it lazily. */
  let externalAuth: ExternalAuthProvider;

  /** An external provider with a live session that refuses attribute writes. */
  function attributeRefusingProvider(): ExternalAuthProvider & { updateProfileAttributes: ReturnType<typeof vi.fn> } {
    const provider = new NoopExternalAuthProvider() as unknown as ExternalAuthProvider & {
      available: boolean;
      updateProfileAttributes: ReturnType<typeof vi.fn>;
    };
    Object.defineProperty(provider, 'available', { value: true });
    provider.fetchSession = vi.fn().mockResolvedValue({ accessToken: 'access', idToken: 'id' });
    provider.updateProfileAttributes = vi.fn().mockRejectedValue(new Error('attribute write refused'));
    return provider;
  }

  async function openAndWaitForProfile(fixture: ReturnType<typeof TestBed.createComponent<ProfileSettingsModalComponent>>): Promise<void> {
    const component = fixture.componentInstance;
    component.modal.open('profile');
    fixture.detectChanges();
    await vi.waitFor(() => expect(component.loadingProfile()).toBe(false));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    user = {
      id: 'user-1',
      username: 'testuser',
      email: 'testuser@example.com',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    };
    externalAuth = new NoopExternalAuthProvider();
    logger = { error: vi.fn() };
    authService = {
      getUserProfile: vi.fn().mockReturnValue(of(user)),
      updateProfile: vi.fn().mockReturnValue(of(user)),
      updatePassword: vi.fn().mockReturnValue(of({ message: 'ok' }) as any),
      logoutPreferred: vi.fn(),
    };
    const themeService = {
      mode: signal<'system' | 'light' | 'dark'>('system'),
      setTheme: vi.fn(),
    };
    const preferences = {
      id: 'prefs-1',
      user_id: 'user-1',
      default_model_id: 'model-1',
      default_personality_id: 'personality-1',
      favorite_model_ids: [],
    };
    preferencesService = {
      getUserPreferences: vi.fn().mockReturnValue(of(preferences)),
      updateUserPreferences: vi.fn().mockImplementation(updated => of(updated)),
    };
    const modelService = {
      getModels: vi.fn().mockReturnValue(
        of([
          { id: 'model-1', name: 'model-one', display_name: 'Model One', description: '', tool_support: true },
          { id: 'model-2', name: 'model-two', display_name: 'Model Two', description: '', tool_support: true },
        ]),
      ),
    };
    personalityService = {
      listPersonalities: vi.fn().mockReturnValue(
        of({
          results: [{ id: 'personality-1', name: 'Vera' }],
          total_count: 1,
          page: 1,
        }),
      ),
    };
    router = { navigate: vi.fn().mockName('Router.navigate') };
    providerAccess = signal<ProviderAccess | null>({ keys_from: 'deployment', providers: ['openai'] });
    providerKeys = {
      access: providerAccess,
      accountsSupplyKeys: computed(() => providerAccess()?.keys_from === 'account'),
      refreshAccess: vi.fn().mockReturnValue(of(null)),
      list: vi.fn().mockReturnValue(of([])),
    };

    await TestBed.configureTestingModule({
      imports: [ProfileSettingsModalComponent],
      providers: [
        provideZonelessChangeDetection(),
        ProfileSettingsModalService,
        { provide: AuthService, useValue: authService },
        { provide: ThemeService, useValue: themeService },
        { provide: UserPreferencesService, useValue: preferencesService },
        { provide: ModelService, useValue: modelService },
        { provide: PersonalityService, useValue: personalityService },
        { provide: Router, useValue: router },
        { provide: ExternalAuthProvider, useFactory: () => externalAuth },
        { provide: LoggerService, useValue: logger },
        { provide: ProviderKeysService, useValue: providerKeys },
      ],
    }).compileComponents();
  });

  it('exposes only the profile tab when the deployment supplies keys', () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    expect(component).toBeTruthy();
    expect(component.tabs().map(t => t.id)).toEqual(['profile']);
  });

  it('hides the tab bar while there is only a single tab', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    expect(fixture.nativeElement.querySelector('.profile-settings__tabs')).toBeNull();
  });

  it('adds an API keys tab when accounts bring their own keys', async () => {
    providerAccess.set({ keys_from: 'account', providers: [] });
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    expect(fixture.componentInstance.tabs().map(t => t.id)).toEqual(['profile', 'api-keys']);
    const tabs = fixture.nativeElement.querySelectorAll('.profile-settings__tab') as NodeListOf<HTMLButtonElement>;
    expect([...tabs].map(t => t.textContent?.trim())).toEqual(['Profile', 'API keys']);
    expect(providerKeys.refreshAccess).toHaveBeenCalled();
  });

  it('shows the key list on the API keys tab', async () => {
    providerAccess.set({ keys_from: 'account', providers: [] });
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    fixture.componentInstance.modal.open('api-keys');
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('app-provider-keys-section')).not.toBeNull();
    expect(fixture.nativeElement.querySelector('.profile-settings__identity-card')).toBeNull();
  });

  it('renders default model and default personality settings', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    const text = fixture.nativeElement.textContent ?? '';
    expect(text).toContain('Default model');
    expect(text).toContain('Model One');
    expect(text).toContain('Default personality');
    expect(text).toContain('Vera');
  });

  it('groups model and personality as chat defaults and explains their scope', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    const defaults = fixture.nativeElement.querySelector('[data-testid="chat-defaults"]') as HTMLElement | null;
    expect(defaults).not.toBeNull();
    expect(defaults?.textContent).toContain('Thread defaults');
    expect(defaults?.textContent).toContain('Choose what new threads start with.');
    expect(defaults?.textContent).toContain('Default model');
    expect(defaults?.textContent).toContain('Default personality');
    expect(defaults?.textContent).toContain('No default personality');
  });

  it('keeps auto-saving chat defaults outside the profile submit form', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    const defaults = fixture.nativeElement.querySelector('[data-testid="chat-defaults"]') as HTMLElement | null;
    expect(defaults).not.toBeNull();
    expect(defaults?.closest('form')).toBeNull();
    expect(defaults?.querySelector('button[type="submit"]')).toBeNull();
  });

  it('persists a changed default model without dropping other preferences', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    await openAndWaitForProfile(fixture);

    await component.onDefaultModelChange('model-2');

    expect(preferencesService.updateUserPreferences).toHaveBeenCalledWith(
      expect.objectContaining({
        user_id: 'user-1',
        default_model_id: 'model-2',
        default_personality_id: 'personality-1',
        favorite_model_ids: [],
      }),
    );
  });

  it('shows the saved defaults in the selects, not the first option', async () => {
    preferencesService.getUserPreferences.mockReturnValue(
      of({
        id: 'prefs-1',
        user_id: 'user-1',
        default_model_id: 'model-2',
        default_personality_id: 'personality-1',
        favorite_model_ids: [],
      }),
    );
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    const modelSelect = fixture.nativeElement.querySelector('[data-testid="default-model-select"]') as HTMLSelectElement;
    const personalitySelect = fixture.nativeElement.querySelector(
      '[data-testid="default-personality-select"]',
    ) as HTMLSelectElement;
    expect(modelSelect.value).toBe('model-2');
    expect(personalitySelect.value).toBe('personality-1');
  });

  it('shows a placeholder rather than another model when the saved default is not listed', async () => {
    preferencesService.getUserPreferences.mockReturnValue(
      of({ id: 'prefs-1', user_id: 'user-1', default_model_id: 'retired-model', favorite_model_ids: [] }),
    );
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    const modelSelect = fixture.nativeElement.querySelector('[data-testid="default-model-select"]') as HTMLSelectElement;
    expect(modelSelect.value).toBe('');
    expect(modelSelect.selectedOptions[0]?.textContent).toContain('Current default is unavailable');
  });

  it('merges a default change onto freshly read preferences so it cannot undo other writes', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    await openAndWaitForProfile(fixture);
    // Another writer (theme toggle, model-picker favorites) changed the record after the modal loaded.
    preferencesService.getUserPreferences.mockReturnValue(
      of({
        id: 'prefs-1',
        user_id: 'user-1',
        default_model_id: 'model-1',
        default_personality_id: 'personality-1',
        theme: 'light',
        favorite_model_ids: ['model-2'],
      }),
    );

    await component.onDefaultModelChange('model-2');

    expect(preferencesService.updateUserPreferences).toHaveBeenLastCalledWith(
      expect.objectContaining({ default_model_id: 'model-2', theme: 'light', favorite_model_ids: ['model-2'] }),
    );
  });

  it('saves the default reasoning effort and agent loop, sending an empty string to clear', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    await openAndWaitForProfile(fixture);

    await component.onDefaultReasoningEffortChange('high');
    expect(preferencesService.updateUserPreferences).toHaveBeenLastCalledWith(
      expect.objectContaining({ reasoning_effort: 'high', default_model_id: 'model-1' }),
    );

    await component.onDefaultAgentLoopChange('advanced');
    expect(preferencesService.updateUserPreferences).toHaveBeenLastCalledWith(expect.objectContaining({ agent_loop: 'advanced' }));

    await component.onDefaultReasoningEffortChange('');
    expect(preferencesService.updateUserPreferences).toHaveBeenLastCalledWith(expect.objectContaining({ reasoning_effort: '' }));

    await component.onDefaultAgentLoopChange('bogus');
    expect(preferencesService.updateUserPreferences).toHaveBeenLastCalledWith(expect.objectContaining({ agent_loop: '' }));
  });

  it('clears the default personality by omitting it from the serialized update', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    await openAndWaitForProfile(fixture);

    await component.onDefaultPersonalityChange('');

    const update = preferencesService.updateUserPreferences.mock.calls.at(-1)?.[0];
    expect(update).toEqual(
      expect.objectContaining({
        user_id: 'user-1',
        default_model_id: 'model-1',
        favorite_model_ids: [],
      }),
    );
    expect(update).not.toHaveProperty('default_personality_id');
    expect(JSON.stringify(update)).not.toContain('default_personality_id');
  });

  it('loads every personality page for the default selector', async () => {
    personalityService.listPersonalities.mockImplementation((page: number) =>
      of(
        page === 1
          ? {
              results: Array.from({ length: 100 }, (_, index) => ({ id: `personality-${index}`, name: `Personality ${index}` })),
              total_count: 101,
              page: 1,
            }
          : {
              results: [{ id: 'personality-100', name: 'Personality 100' }],
              total_count: 101,
              page: 2,
            },
      ),
    );
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;

    await openAndWaitForProfile(fixture);

    expect(personalityService.listPersonalities).toHaveBeenNthCalledWith(1, 1, 100);
    expect(personalityService.listPersonalities).toHaveBeenNthCalledWith(2, 2, 100);
    expect(component.personalities()).toHaveLength(101);
  });

  it('loads the profile when the modal opens', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    await openAndWaitForProfile(fixture);
    expect(component.currentUser()?.email).toBe('testuser@example.com');
  });

  it('places Export Data at the end of the settings form and opens Import & Export', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    await openAndWaitForProfile(fixture);

    const row = fixture.nativeElement.querySelector('.profile-settings__data-action') as HTMLElement;
    expect(row.textContent).toContain('Export Data');
    const button = row.querySelector('button') as HTMLButtonElement;
    expect(button.textContent).toContain('Export');

    button.click();
    expect(router.navigate).toHaveBeenCalledWith(['/data']);
  });

  describe('image generation', () => {
    function withImageProviders(chosen: string, offered: string[]): void {
      preferencesService.getUserPreferences.mockReturnValue(
        of({
          id: 'prefs-1',
          user_id: 'user-1',
          default_model_id: 'model-1',
          default_personality_id: 'personality-1',
          favorite_model_ids: [],
          image_generation_provider: chosen,
          image_generation_providers: offered,
        }),
      );
    }

    function imageSection(fixture: { nativeElement: HTMLElement }): HTMLElement | null {
      return fixture.nativeElement.querySelector('[data-testid="image-generation"]');
    }

    it('is hidden when the deployment offers no choice of provider', async () => {
      withImageProviders('openai', ['openai']);
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      expect(imageSection(fixture)).toBeNull();
    });

    it('offers the configured providers with the stored pick selected', async () => {
      withImageProviders('zai', ['openai', 'google', 'zai', 'qwen']);
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      const select = imageSection(fixture)?.querySelector('select') as HTMLSelectElement | null;
      expect(select).not.toBeNull();
      expect(Array.from(select!.options).map(o => o.textContent?.trim())).toEqual([
        'OpenAI (GPT Image)',
        'Google (Gemini image)',
        'Z.ai (GLM-Image)',
        'Qwen (Qwen-Image)',
      ]);
      expect(select!.value).toBe('zai');
    });

    it('shows the provider the server falls back to when the stored pick is not offered', async () => {
      withImageProviders('qwen', ['google', 'zai']);
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      expect(fixture.componentInstance.imageGenerationProvider()).toBe('google');
    });

    it('saves a new provider without dropping other preferences', async () => {
      withImageProviders('openai', ['openai', 'google']);
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      await fixture.componentInstance.onImageGenerationProviderChange('google');

      expect(preferencesService.updateUserPreferences).toHaveBeenCalledWith(
        expect.objectContaining({
          default_model_id: 'model-1',
          default_personality_id: 'personality-1',
          image_generation_provider: 'google',
        }),
      );
      expect(fixture.componentInstance.imageGenerationProvider()).toBe('google');
    });
  });

  describe('timezone', () => {
    const timezoneInput = (fixture: ReturnType<typeof TestBed.createComponent<ProfileSettingsModalComponent>>) =>
      fixture.nativeElement.querySelector('select[formControlName="timezone"]') as HTMLSelectElement;

    const mockBrowserTimezone = (timeZone: string): void => {
      vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({
        timeZone,
      } as unknown as Intl.ResolvedDateTimeFormatOptions);
    };

    afterEach(() => {
      vi.restoreAllMocks();
    });

    const timezoneOffer = (fixture: ReturnType<typeof TestBed.createComponent<ProfileSettingsModalComponent>>) =>
      fixture.nativeElement.querySelector('[data-testid="timezone-offer"]') as HTMLElement | null;

    it('shows the saved timezone with no offer when the browser agrees', async () => {
      mockBrowserTimezone('America/Chicago');
      user['timezone'] = 'America/Chicago';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      expect(timezoneInput(fixture).value).toBe('America/Chicago');
      expect(timezoneOffer(fixture)).toBeNull();
    });

    it('offers every zone in a select and keeps a saved zone the browser does not list', async () => {
      user['timezone'] = 'Legacy/Zone';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      const select = timezoneInput(fixture);
      const values = Array.from(select.options).map(option => option.value);
      expect(values).toContain('America/New_York');
      expect(values).toContain('UTC');
      expect(values).toContain('Legacy/Zone');
      expect(select.value).toBe('Legacy/Zone');
    });

    // The server gives every account a timezone (America/New_York by default), so a saved
    // zone that differs from the browser's is the normal case for a user who never chose one.
    it('offers the browser timezone when it differs from the saved one, without changing the form', async () => {
      mockBrowserTimezone('Europe/Berlin');
      user['timezone'] = 'America/New_York';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);

      expect(timezoneInput(fixture).value).toBe('America/New_York');
      expect(timezoneOffer(fixture)?.textContent).toContain('Your browser is set to Europe/Berlin.');
      expect(component.profileForm.pristine).toBe(true);
      expect(authService.updateProfile).not.toHaveBeenCalled();
    });

    it('does not slip the browser timezone into an unrelated save', async () => {
      mockBrowserTimezone('Europe/Berlin');
      user['timezone'] = 'America/New_York';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);

      component.profileForm.patchValue({ first_name: 'Ada' });
      await component.saveProfile();

      expect(authService.updateProfile).toHaveBeenCalledWith(expect.objectContaining({ first_name: 'Ada', timezone: 'America/New_York' }));
    });

    it('offers nothing when the browser reports an unusable zone', async () => {
      mockBrowserTimezone('Etc/Unknown');
      user['timezone'] = 'America/New_York';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      await openAndWaitForProfile(fixture);

      expect(timezoneOffer(fixture)).toBeNull();
      expect(fixture.componentInstance.profileForm.valid).toBe(true);
    });

    it('saves the timezone through the profile API on Save Changes', async () => {
      user['timezone'] = 'America/Chicago';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);

      component.profileForm.patchValue({ timezone: ' Asia/Tokyo ' });
      await component.saveProfile();

      expect(authService.updateProfile).toHaveBeenCalledWith(
        expect.objectContaining({ email: 'testuser@example.com', timezone: 'Asia/Tokyo' }),
      );
      expect(component.message()?.type).toBe('success');
    });

    it('selects the offered browser timezone on click and saves it on Save Changes', async () => {
      mockBrowserTimezone('Europe/Berlin');
      user['timezone'] = 'America/New_York';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);

      (timezoneOffer(fixture)?.querySelector('button') as HTMLButtonElement).click();
      fixture.detectChanges();

      expect(timezoneInput(fixture).value).toBe('Europe/Berlin');
      expect(component.profileForm.dirty).toBe(true);
      expect(timezoneOffer(fixture)).toBeNull();
      expect(authService.updateProfile).not.toHaveBeenCalled();

      await component.saveProfile();

      expect(authService.updateProfile).toHaveBeenCalledWith(expect.objectContaining({ timezone: 'Europe/Berlin' }));
    });

    it('rejects an unknown timezone without calling the API', async () => {
      user['timezone'] = 'America/Chicago';
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);

      component.profileForm.patchValue({ timezone: 'Mars/Olympus_Mons' });
      await component.saveProfile();
      fixture.detectChanges();

      expect(component.profileForm.controls.timezone.hasError('timezone')).toBe(true);
      expect(authService.updateProfile).not.toHaveBeenCalled();
      expect(fixture.nativeElement.textContent).toContain('Enter a valid IANA timezone');
    });

    it('surfaces the server error when saving fails', async () => {
      user['timezone'] = 'America/Chicago';
      authService.updateProfile.mockReturnValue(throwError(() => ({ error: { error: 'Invalid timezone' } })));
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);

      await component.saveProfile();

      expect(component.message()).toEqual({ type: 'error', text: 'Invalid timezone' });
    });
  });

  describe('saving the profile', () => {
    async function openAndEditName(): Promise<ProfileSettingsModalComponent> {
      const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
      const component = fixture.componentInstance;
      await openAndWaitForProfile(fixture);
      component.profileForm.patchValue({ first_name: ' Ada ', last_name: 'Lovelace' });
      return component;
    }

    it('saves through the profile API and reports success', async () => {
      const component = await openAndEditName();

      await component.saveProfile();

      expect(authService.updateProfile).toHaveBeenCalledWith({
        email: 'testuser@example.com',
        first_name: 'Ada',
        last_name: 'Lovelace',
      });
      expect(component.message()).toEqual({ type: 'success', text: 'Profile updated.' });
    });

    it('still saves when the identity provider rejects the name update', async () => {
      const provider = attributeRefusingProvider();
      externalAuth = provider;
      const component = await openAndEditName();

      await component.saveProfile();

      expect(authService.updateProfile).toHaveBeenCalled();
      expect(provider.updateProfileAttributes).toHaveBeenCalledWith({ given_name: 'Ada', family_name: 'Lovelace' });
      expect(component.message()).toEqual({ type: 'success', text: 'Profile updated.' });
      expect(logger.error).toHaveBeenCalled();
    });

    it('shows the server error and skips the provider update when the save fails', async () => {
      const provider = attributeRefusingProvider();
      externalAuth = provider;
      authService.updateProfile.mockReturnValue(throwError(() => new Error('Email already in use')));
      const component = await openAndEditName();

      await component.saveProfile();

      expect(component.message()).toEqual({ type: 'error', text: 'Email already in use' });
      expect(provider.updateProfileAttributes).not.toHaveBeenCalled();
    });
  });
});

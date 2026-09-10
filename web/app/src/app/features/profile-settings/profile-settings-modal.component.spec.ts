import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ProfileSettingsModalComponent } from './profile-settings-modal.component';
import { ProfileSettingsModalService } from './profile-settings-modal.service';
import { AuthService } from '../../core/services/auth.service';
import { ExternalAuthProvider, NoopExternalAuthProvider } from '../../core/auth/external-auth.provider';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ThemeService } from '../../core/services/theme.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';

describe('ProfileSettingsModalComponent (open-source profile-only)', () => {
  let preferencesService: {
    getUserPreferences: ReturnType<typeof vi.fn>;
    updateUserPreferences: ReturnType<typeof vi.fn>;
  };
  let personalityService: {
    listPersonalities: ReturnType<typeof vi.fn>;
  };

  async function openAndWaitForProfile(fixture: ReturnType<typeof TestBed.createComponent<ProfileSettingsModalComponent>>): Promise<void> {
    const component = fixture.componentInstance;
    component.modal.open('profile');
    fixture.detectChanges();
    await vi.waitFor(() => expect(component.loadingProfile()).toBe(false));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    const user = {
      id: 'user-1',
      username: 'testuser',
      email: 'testuser@example.com',
      created_at: '2024-01-01T00:00:00Z',
      updated_at: '2024-01-01T00:00:00Z',
    };
    const authService = {
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
      getModels: vi.fn().mockReturnValue(of([
        { id: 'model-1', name: 'model-one', display_name: 'Model One', description: '', tool_support: true },
        { id: 'model-2', name: 'model-two', display_name: 'Model Two', description: '', tool_support: true },
      ])),
    };
    personalityService = {
      listPersonalities: vi.fn().mockReturnValue(of({
        results: [{ id: 'personality-1', name: 'Vera' }],
        total_count: 1,
        page: 1,
      })),
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
        { provide: ExternalAuthProvider, useClass: NoopExternalAuthProvider },
      ],
    }).compileComponents();
  });

  it('creates and exposes only the profile tab', () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    expect(component).toBeTruthy();
    expect(component.tabs.map(t => t.id)).toEqual(['profile']);
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
    expect(defaults?.textContent).toContain('Chat defaults');
    expect(defaults?.textContent).toContain('Choose what new chats start with.');
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

    expect(preferencesService.updateUserPreferences).toHaveBeenCalledWith(expect.objectContaining({
      user_id: 'user-1',
      default_model_id: 'model-2',
      default_personality_id: 'personality-1',
      favorite_model_ids: [],
    }));
  });

  it('clears the default personality by omitting it from the serialized update', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    await openAndWaitForProfile(fixture);

    await component.onDefaultPersonalityChange('');

    const update = preferencesService.updateUserPreferences.mock.calls.at(-1)?.[0];
    expect(update).toEqual(expect.objectContaining({
      user_id: 'user-1',
      default_model_id: 'model-1',
      favorite_model_ids: [],
    }));
    expect(update).not.toHaveProperty('default_personality_id');
    expect(JSON.stringify(update)).not.toContain('default_personality_id');
  });

  it('loads every personality page for the default selector', async () => {
    personalityService.listPersonalities.mockImplementation((page: number) => of(
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
    ));
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
});

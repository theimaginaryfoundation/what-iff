import { signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ProfileSettingsModalComponent } from './profile-settings-modal.component';
import { ProfileSettingsModalService } from './profile-settings-modal.service';
import { AuthService } from '../../core/services/auth.service';
import { ExternalAuthProvider, NoopExternalAuthProvider } from '../../core/auth/external-auth.provider';
import { ThemeService } from '../../core/services/theme.service';

describe('ProfileSettingsModalComponent (open-source profile-only)', () => {
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

    await TestBed.configureTestingModule({
      imports: [ProfileSettingsModalComponent],
      providers: [
        provideZonelessChangeDetection(),
        ProfileSettingsModalService,
        { provide: AuthService, useValue: authService },
        { provide: ThemeService, useValue: themeService },
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

  it('loads the profile when the modal opens', async () => {
    const fixture = TestBed.createComponent(ProfileSettingsModalComponent);
    const component = fixture.componentInstance;
    component.modal.open('profile');
    fixture.detectChanges();
    await fixture.whenStable();
    expect(component.currentUser()?.email).toBe('testuser@example.com');
  });
});

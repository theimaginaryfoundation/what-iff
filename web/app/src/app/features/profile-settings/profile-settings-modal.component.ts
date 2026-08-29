import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, effect, inject, signal } from '@angular/core';
import { AbstractControl, FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { firstValueFrom } from 'rxjs';

import { ExternalAuthProvider } from '../../core/auth/external-auth.provider';
import { Model } from '../../core/models/model.model';
import { Personality } from '../../core/models/personality.model';
import { UpdatePasswordRequest, UpdateUserRequest, UserPreferences, UserResponse } from '../../core/models/user.model';
import { AuthService } from '../../core/services/auth.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ThemeMode, ThemeService } from '../../core/services/theme.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { UserIconComponent } from '../../shared/ui/icons/icons';
import { ProfileSettingsModalService, ProfileSettingsTab } from './profile-settings-modal.service';

/** Profile & Settings modal: account identity, appearance, defaults, and password. */
@Component({
  selector: 'app-profile-settings-modal',
  standalone: true,
  imports: [CommonModule, ModalComponent, ReactiveFormsModule, UserIconComponent],
  templateUrl: './profile-settings-modal.component.html',
  styleUrl: './profile-settings-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProfileSettingsModalComponent {
  readonly modal = inject(ProfileSettingsModalService);
  private readonly authService = inject(AuthService);
  private readonly externalAuth = inject(ExternalAuthProvider);
  private readonly fb = inject(FormBuilder);
  private readonly themeService = inject(ThemeService);
  private readonly preferencesService = inject(UserPreferencesService);
  private readonly modelService = inject(ModelService);
  private readonly personalityService = inject(PersonalityService);

  readonly tabs: Array<{ id: ProfileSettingsTab; label: string; icon: 'profile' }> = [
    { id: 'profile', label: 'Profile', icon: 'profile' },
  ];

  readonly profileForm = this.fb.group({
    email: ['', [Validators.required, Validators.email, Validators.maxLength(255)]],
    first_name: ['', [Validators.maxLength(50)]],
    last_name: ['', [Validators.maxLength(50)]],
  });
  readonly passwordForm = this.fb.group({
    current_password: [''],
    new_password: ['', [Validators.minLength(8)]],
    confirm_password: [''],
  }, { validators: this.passwordMatchValidator });

  readonly currentUser = signal<UserResponse | null>(null);
  readonly preferences = signal<UserPreferences | null>(null);
  readonly models = signal<readonly Model[]>([]);
  readonly personalities = signal<readonly Personality[]>([]);
  readonly themeMode = signal<ThemeMode>('system');
  readonly loadingProfile = signal(false);
  readonly loadingAction = signal(false);
  readonly message = signal<{ type: 'success' | 'error' | 'info'; text: string } | null>(null);

  private hasLoadedForOpen = false;

  readonly displayName = computed(() => {
    const user = this.currentUser();
    if (!user) return 'Your profile';
    const composed = [user.first_name, user.last_name].filter(Boolean).join(' ').trim();
    return composed || user.username || user.email;
  });

  readonly userInitials = computed(() => {
    const user = this.currentUser();
    if (!user) return '?';
    const parts = [user.first_name, user.last_name].filter(Boolean);
    if (parts.length > 0) return parts.map(part => String(part).charAt(0).toUpperCase()).join('').slice(0, 2);
    return (user.username || user.email || '?').slice(0, 2).toUpperCase();
  });

  constructor() {
    effect(() => {
      if (this.modal.openState()) {
        if (!this.hasLoadedForOpen) {
          this.hasLoadedForOpen = true;
          void this.loadProfile();
        }
      } else {
        this.hasLoadedForOpen = false;
        this.message.set(null);
      }
    });
  }

  close(): void { this.modal.close(); }
  setTab(tab: ProfileSettingsTab): void { this.modal.setTab(tab); }

  async onThemeModeChange(mode: ThemeMode): Promise<void> {
    if (this.themeMode() === mode) return;
    this.themeMode.set(mode);
    this.themeService.setTheme(mode, true);
    this.message.set({ type: 'success', text: 'Theme preference updated.' });
  }

  async onDefaultModelChange(modelId: string): Promise<void> {
    await this.savePreferences({ default_model_id: modelId }, 'Default model updated.');
  }

  async onDefaultPersonalityChange(personalityId: string): Promise<void> {
    await this.savePreferences(
      { default_personality_id: personalityId || undefined },
      'Default personality updated.',
    );
  }

  async saveProfile(): Promise<void> {
    if (this.profileForm.invalid) {
      this.profileForm.markAllAsTouched();
      return;
    }
    this.loadingAction.set(true);
    this.message.set(null);
    const firstName = (this.profileForm.value.first_name ?? '').trim();
    const lastName = (this.profileForm.value.last_name ?? '').trim();
    const updateData: UpdateUserRequest = {
      email: (this.profileForm.value.email ?? '').trim(),
      first_name: firstName || undefined,
      last_name: lastName || undefined,
    };
    try {
      if (await this.isExternalAuthenticated()) await this.syncExternalProfile(firstName, lastName);
      const user = await firstValueFrom(this.authService.updateProfile(updateData));
      this.currentUser.set(user);
      this.profileForm.markAsPristine();
      this.message.set({ type: 'success', text: 'Profile updated.' });
    } catch (error) {
      this.message.set({ type: 'error', text: this.formatProfileError(error) });
    } finally {
      this.loadingAction.set(false);
    }
  }

  async savePassword(): Promise<void> {
    const currentPassword = this.passwordForm.value.current_password?.trim() ?? '';
    const newPassword = this.passwordForm.value.new_password?.trim() ?? '';
    const confirmPassword = this.passwordForm.value.confirm_password?.trim() ?? '';
    if (!currentPassword && !newPassword && !confirmPassword) return;
    if (!currentPassword || !newPassword || !confirmPassword || this.passwordForm.invalid) {
      this.passwordForm.markAllAsTouched();
      this.message.set({ type: 'error', text: 'Enter your current password and matching new password.' });
      return;
    }
    this.loadingAction.set(true);
    this.message.set(null);
    const passwordData: UpdatePasswordRequest = { current_password: currentPassword, new_password: newPassword };
    try {
      if (await this.isExternalAuthenticated()) {
        await this.externalAuth.updatePassword(passwordData.current_password, passwordData.new_password);
      } else {
        await firstValueFrom(this.authService.updatePassword(passwordData));
      }
      this.passwordForm.reset();
      this.message.set({ type: 'success', text: 'Password updated.' });
    } catch (error) {
      this.message.set({ type: 'error', text: this.formatPasswordError(error) });
    } finally {
      this.loadingAction.set(false);
    }
  }

  logout(): void {
    this.close();
    void this.authService.logoutPreferred();
  }

  formatDate(value?: string): string {
    if (!value) return 'N/A';
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return 'N/A';
    return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
  }

  private async loadProfile(): Promise<void> {
    this.loadingProfile.set(true);
    try {
      const [user, preferences, models, personalityPage] = await Promise.all([
        firstValueFrom(this.authService.getUserProfile()),
        firstValueFrom(this.preferencesService.getUserPreferences()),
        firstValueFrom(this.modelService.getModels()),
        firstValueFrom(this.personalityService.listPersonalities(1, 100)),
      ]);
      this.currentUser.set(user);
      this.preferences.set(preferences);
      this.models.set(models);
      this.personalities.set(personalityPage.results);
      this.profileForm.patchValue({
        email: user.email || '',
        first_name: user.first_name || '',
        last_name: user.last_name || '',
      });
      this.themeMode.set(this.themeService.mode());
      this.profileForm.markAsPristine();
    } catch (error) {
      this.message.set({ type: 'error', text: this.getErrorMessage(error, 'Failed to load profile settings.') });
    } finally {
      this.loadingProfile.set(false);
    }
  }

  private async savePreferences(
    patch: Partial<Pick<UserPreferences, 'default_model_id' | 'default_personality_id'>>,
    successMessage: string,
  ): Promise<void> {
    const current = this.preferences();
    if (!current) return;
    this.loadingAction.set(true);
    this.message.set(null);
    try {
      const updated = await firstValueFrom(
        this.preferencesService.updateUserPreferences({ ...current, ...patch }),
      );
      this.preferences.set(updated);
      this.message.set({ type: 'success', text: successMessage });
    } catch (error) {
      this.message.set({ type: 'error', text: this.getErrorMessage(error, 'Failed to update defaults.') });
    } finally {
      this.loadingAction.set(false);
    }
  }

  private passwordMatchValidator(form: AbstractControl): { passwordMismatch: true } | null {
    const newPassword = form.get('new_password')?.value;
    const confirmControl = form.get('confirm_password');
    const confirmPassword = confirmControl?.value;
    if (newPassword && confirmPassword && newPassword !== confirmPassword) {
      const existingErrors = confirmControl?.errors ?? {};
      confirmControl?.setErrors({ ...existingErrors, passwordMismatch: true });
      return { passwordMismatch: true };
    }
    if (confirmControl?.errors?.['passwordMismatch']) {
      const { passwordMismatch, ...rest } = confirmControl.errors;
      confirmControl.setErrors(Object.keys(rest).length > 0 ? rest : null);
    }
    return null;
  }

  private async isExternalAuthenticated(): Promise<boolean> {
    if (!this.externalAuth.available) return false;
    try {
      const tokens = await this.externalAuth.fetchSession();
      return Boolean(tokens?.accessToken);
    } catch {
      return false;
    }
  }

  private async syncExternalProfile(firstName: string, lastName: string): Promise<void> {
    const attributes: Record<string, string> = {};
    if (firstName) attributes['given_name'] = firstName;
    if (lastName) attributes['family_name'] = lastName;
    if (Object.keys(attributes).length === 0) return;
    await this.externalAuth.updateProfileAttributes(attributes);
  }

  private formatProfileError(error: unknown): string {
    const name = (error as { name?: string })?.name;
    if (name === 'InvalidParameterException') return 'Name contains invalid characters or is too long.';
    if (name === 'NotAuthorizedException') return 'Session expired while updating profile. Please sign in again.';
    return this.getErrorMessage(error, 'Failed to update profile.');
  }

  private formatPasswordError(error: unknown): string {
    const name = (error as { name?: string })?.name;
    if (name === 'NotAuthorizedException') return 'Current password is incorrect.';
    if (name === 'InvalidPasswordException') return this.getErrorMessage(error, 'New password does not meet complexity requirements.');
    return this.getErrorMessage(error, 'Failed to update password.');
  }

  private getErrorMessage(error: unknown, fallback: string): string {
    const err = error as { error?: { error?: string; message?: string }; message?: string };
    return err?.error?.error || err?.error?.message || err?.message || fallback;
  }
}

import { CommonModule } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, effect, inject, signal } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { AbstractControl, FormBuilder, ReactiveFormsModule, Validators } from '@angular/forms';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ExternalAuthProvider } from '../../core/auth/external-auth.provider';
import { Model } from '../../core/models/model.model';
import { Personality } from '../../core/models/personality.model';
import {
  AGENT_LOOP_DESCRIPTIONS,
  AGENT_LOOP_LABELS,
  REASONING_EFFORT_DESCRIPTION,
  REASONING_EFFORT_OPTIONS,
  parseAgentLoop,
  parseReasoningEffort,
} from '../../core/models/turn-settings.model';
import {
  IMAGE_GENERATION_PROVIDER_LABELS,
  ImageGenerationProvider,
  UpdatePasswordRequest,
  UpdateUserRequest,
  UserPreferences,
  UserPreferencesUpdateRequest,
  UserResponse,
} from '../../core/models/user.model';
import { AuthService } from '../../core/services/auth.service';
import { LoggerService } from '../../core/services/logger.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { ProviderKeysService } from '../../core/services/provider-keys.service';
import { ThemeMode, ThemeService } from '../../core/services/theme.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { hasDefaultModel } from '../../core/utils/default-model';
import { TIMEZONE_MAX_LENGTH, detectBrowserTimezone, listTimezones, timezoneValidator } from '../../core/utils/timezone.helpers';
import { ModalComponent } from '../../shared/ui/modal/modal.component';
import { ShieldIconComponent, UserIconComponent } from '../../shared/ui/icons/icons';
import { TooltipDirective } from '../../shared/ui/tooltip/tooltip.directive';
import { ProfileSettingsModalService, ProfileSettingsTab } from './profile-settings-modal.service';
import { ProvidersSectionComponent } from './providers/providers-section.component';

/** Profile & Settings modal: account identity, appearance, defaults, and password. */
@Component({
  selector: 'app-profile-settings-modal',
  standalone: true,
  imports: [CommonModule, ModalComponent, ReactiveFormsModule, UserIconComponent, ShieldIconComponent, TooltipDirective, ProvidersSectionComponent],
  templateUrl: './profile-settings-modal.component.html',
  styleUrl: './profile-settings-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ProfileSettingsModalComponent {
  readonly modal = inject(ProfileSettingsModalService);
  private readonly providerKeys = inject(ProviderKeysService);
  private readonly authService = inject(AuthService);
  private readonly externalAuth = inject(ExternalAuthProvider);
  private readonly fb = inject(FormBuilder);
  private readonly themeService = inject(ThemeService);
  private readonly preferencesService = inject(UserPreferencesService);
  private readonly modelService = inject(ModelService);
  private readonly personalityService = inject(PersonalityService);
  private readonly router = inject(Router);
  private readonly logger = inject(LoggerService);

  /** The Providers tab appears only where accounts bring their own provider keys. */
  readonly tabs = computed<Array<{ id: ProfileSettingsTab; label: string; icon: 'profile' | 'providers' }>>(() => [
    { id: 'profile', label: 'Profile', icon: 'profile' },
    ...(this.providerKeys.accountsSupplyKeys() ? [{ id: 'providers' as const, label: 'Providers', icon: 'providers' as const }] : []),
  ]);

  readonly profileForm = this.fb.group({
    email: ['', [Validators.required, Validators.email, Validators.maxLength(255)]],
    first_name: ['', [Validators.maxLength(50)]],
    last_name: ['', [Validators.maxLength(50)]],
    timezone: ['', [Validators.maxLength(TIMEZONE_MAX_LENGTH), timezoneValidator]],
  });
  readonly passwordForm = this.fb.group(
    {
      current_password: [''],
      new_password: ['', [Validators.minLength(8)]],
      confirm_password: [''],
    },
    { validators: this.passwordMatchValidator },
  );

  readonly reasoningEffortOptions = REASONING_EFFORT_OPTIONS;
  readonly reasoningEffortDescription = REASONING_EFFORT_DESCRIPTION;
  readonly agentLoopLabels = AGENT_LOOP_LABELS;
  readonly agentLoopDescriptions = AGENT_LOOP_DESCRIPTIONS;

  readonly currentUser = signal<UserResponse | null>(null);
  readonly preferences = signal<UserPreferences | null>(null);
  /** Image vendors to choose between; the picker is hidden when there is no choice. */
  readonly imageGenerationProviders = computed(() =>
    (this.preferences()?.image_generation_providers ?? []).map(id => ({ id, label: IMAGE_GENERATION_PROVIDER_LABELS[id] ?? id })),
  );
  /** The vendor images actually use: the stored pick when available, else the first offered (the server's fallback). */
  readonly imageGenerationProvider = computed(() => {
    const offered = this.preferences()?.image_generation_providers ?? [];
    const chosen = this.preferences()?.image_generation_provider;
    return chosen && offered.includes(chosen) ? chosen : (offered[0] ?? '');
  });
  readonly models = signal<readonly Model[]>([]);
  /**
   * True when the saved default model is not one the model list offers (retired or no longer
   * available to this account). The select then shows a placeholder instead of silently
   * displaying the first model, which reads as the save not having stuck.
   */
  readonly defaultModelUnlisted = computed(() => {
    const id = this.preferences()?.default_model_id;
    const models = this.models();
    return this.hasDefaultModel() && models.length > 0 && !models.some((model) => model.id === id);
  });
  /**
   * False until the account has a default model: on an install without a model catalog that is
   * until it enables a model on one of its connections (ADR 0x027).
   */
  readonly hasDefaultModel = computed(() => hasDefaultModel(this.preferences()));
  readonly personalities = signal<readonly Personality[]>([]);
  readonly themeMode = signal<ThemeMode>('system');
  /**
   * IANA names offered in the Timezone select. A saved value the browser doesn't list is added on
   * load so the select can still show it.
   */
  readonly timezones = signal<readonly string[]>(listTimezones());
  /**
   * The browser's timezone. Every account has a saved timezone (the server defaults it to
   * America/New_York), so instead of prefilling, the field offers this zone as a one-click choice
   * whenever it differs from the selected one. Nothing is saved until Save Changes.
   */
  readonly browserTimezone = detectBrowserTimezone();
  private readonly selectedTimezone = toSignal(this.profileForm.controls.timezone.valueChanges, { initialValue: '' });
  readonly timezoneOffer = computed(() =>
    this.browserTimezone && this.browserTimezone !== this.selectedTimezone() ? this.browserTimezone : '',
  );
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
    if (parts.length > 0)
      return parts
        .map(part => String(part).charAt(0).toUpperCase())
        .join('')
        .slice(0, 2);
    return (user.username || user.email || '?').slice(0, 2).toUpperCase();
  });

  constructor() {
    effect(() => {
      if (this.modal.openState()) {
        if (!this.hasLoadedForOpen) {
          this.hasLoadedForOpen = true;
          void this.loadProfile();
          this.providerKeys.refreshAccess().subscribe();
        }
      } else {
        this.hasLoadedForOpen = false;
        this.message.set(null);
      }
    });
  }

  close(): void {
    this.modal.close();
  }

  /** Selects the browser's timezone in the form; it is saved with the next Save Changes. */
  useBrowserTimezone(): void {
    const control = this.profileForm.controls.timezone;
    control.setValue(this.browserTimezone);
    control.markAsDirty();
  }
  setTab(tab: ProfileSettingsTab): void {
    this.modal.setTab(tab);
  }

  /** Closes the modal and opens the Import & Export screen. */
  goToData(): void {
    this.close();
    this.router.navigate(['/data']);
  }

  async onThemeModeChange(mode: ThemeMode): Promise<void> {
    if (this.themeMode() === mode) return;
    this.themeMode.set(mode);
    this.themeService.setTheme(mode, true);
    this.message.set({ type: 'success', text: 'Theme preference updated.' });
  }

  async onDefaultModelChange(modelId: string): Promise<void> {
    await this.savePreferences({ default_model_id: modelId }, 'Default model updated.');
  }

  async onImageGenerationProviderChange(provider: ImageGenerationProvider): Promise<void> {
    await this.savePreferences({ image_generation_provider: provider }, 'Image generation provider updated.');
  }

  async onDefaultPersonalityChange(personalityId: string): Promise<void> {
    await this.savePreferences({ default_personality_id: personalityId || undefined }, 'Default personality updated.');
  }

  /** `''` (Model default) clears the account default; the PUT preserves a field only when absent or null. */
  async onDefaultReasoningEffortChange(value: string): Promise<void> {
    await this.savePreferences({ reasoning_effort: parseReasoningEffort(value) ?? '' }, 'Default reasoning effort updated.');
  }

  async onDefaultAgentLoopChange(value: string): Promise<void> {
    await this.savePreferences({ agent_loop: parseAgentLoop(value) ?? '' }, 'Default agent loop updated.');
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
      timezone: (this.profileForm.value.timezone ?? '').trim() || undefined,
    };
    try {
      const user = await firstValueFrom(this.authService.updateProfile(updateData));
      this.currentUser.set(user);
      this.profileForm.markAsPristine();
      await this.mirrorExternalProfile(firstName, lastName);
      this.message.set({ type: 'success', text: 'Profile updated.' });
    } catch (error) {
      this.message.set({ type: 'error', text: this.getErrorMessage(error, 'Failed to update profile.') });
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
      this.message.set({ type: 'error', text: this.getErrorMessage(error, 'Failed to update password.') });
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
      const [user, preferences, models, personalities] = await Promise.all([
        firstValueFrom(this.authService.getUserProfile()),
        firstValueFrom(this.preferencesService.getUserPreferences()),
        firstValueFrom(this.modelService.getModels()),
        this.loadAllPersonalities(),
      ]);
      this.currentUser.set(user);
      this.preferences.set(preferences);
      this.models.set(models);
      this.personalities.set(personalities);
      const savedTimezone = user.timezone || '';
      this.includeTimezoneOptions(savedTimezone, this.browserTimezone);
      this.profileForm.patchValue({
        email: user.email || '',
        first_name: user.first_name || '',
        last_name: user.last_name || '',
        timezone: savedTimezone,
      });
      this.themeMode.set(this.themeService.mode());
      this.profileForm.markAsPristine();
    } catch (error) {
      this.message.set({ type: 'error', text: this.getErrorMessage(error, 'Failed to load profile settings.') });
    } finally {
      this.loadingProfile.set(false);
    }
  }

  /** Adds zones the select must be able to show (e.g. a saved legacy name) that the runtime doesn't list. */
  private includeTimezoneOptions(...zones: string[]): void {
    const missing = zones.filter(zone => zone && !this.timezones().includes(zone));
    if (missing.length > 0) this.timezones.update(list => [...list, ...missing].sort());
  }

  private async loadAllPersonalities(): Promise<readonly Personality[]> {
    const limit = 100;
    const firstPage = await firstValueFrom(this.personalityService.listPersonalities(1, limit));
    const personalities = [...firstPage.results];

    for (let page = 2; personalities.length < firstPage.total_count; page++) {
      const nextPage = await firstValueFrom(this.personalityService.listPersonalities(page, limit));
      personalities.push(...nextPage.results);
      if (nextPage.results.length === 0) break;
    }

    return personalities;
  }

  private async savePreferences(
    patch: Partial<
      Pick<UserPreferencesUpdateRequest, 'default_model_id' | 'default_personality_id' | 'image_generation_provider' | 'reasoning_effort' | 'agent_loop'>
    >,
    successMessage: string,
  ): Promise<void> {
    if (!this.preferences()) return;
    this.loadingAction.set(true);
    this.message.set(null);
    try {
      // PUT replaces the whole record, so merge onto a fresh read rather than the copy loaded
      // when the modal opened: the theme toggle above and the model picker's favorites write
      // preferences through their own read-modify-write, and a stale copy would undo them.
      const current = await firstValueFrom(this.preferencesService.getUserPreferences());
      const updatedPreferences: UserPreferencesUpdateRequest = { ...current, ...patch };
      if ('default_personality_id' in patch && patch.default_personality_id === undefined) {
        delete updatedPreferences.default_personality_id;
      }
      const updated = await firstValueFrom(this.preferencesService.updateUserPreferences(updatedPreferences));
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

  /**
   * Best-effort copy of the saved name to the external identity provider, when one is in use. The
   * profile API is the source of truth, and a provider may refuse attribute writes for some
   * sessions, so this runs after the save and never fails it.
   */
  private async mirrorExternalProfile(firstName: string, lastName: string): Promise<void> {
    const attributes: Record<string, string> = {};
    if (firstName) attributes['given_name'] = firstName;
    if (lastName) attributes['family_name'] = lastName;
    if (Object.keys(attributes).length === 0) return;
    try {
      if (!(await this.isExternalAuthenticated())) return;
      await this.externalAuth.updateProfileAttributes(attributes);
    } catch (error) {
      this.logger.error('Profile saved, but the identity provider name could not be updated', error);
    }
  }

  private getErrorMessage(error: unknown, fallback: string): string {
    const err = error as { error?: { error?: string; message?: string }; message?: string };
    return err?.error?.error || err?.error?.message || err?.message || fallback;
  }
}

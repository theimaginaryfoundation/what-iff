import { Component, DestroyRef, ElementRef, HostListener, OnInit, Renderer2, ViewChild, computed, inject, signal, ChangeDetectionStrategy } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { CommonModule } from '@angular/common';
import { FormBuilder, FormGroup, Validators, ReactiveFormsModule } from '@angular/forms';
import { Router } from '@angular/router';
import { AuthService } from '../../core/services/auth.service';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { MemoryImportResult } from '../../core/models/memory.model';
import { MemoryService } from '../../core/services/memory.service';
import { ThemeService, ThemeMode } from '../../core/services/theme.service';
import { UpdateUserRequest, UpdatePasswordRequest, UserResponse, UserPreferences } from '../../core/models/user.model';
import { Model } from '../../core/models/model.model';
import { Personality } from '../../core/models/personality.model';
import { NULL_PERSONALITY_ID } from '../../core/constants/app.constants';
import { ExternalAuthProvider } from '../../core/auth/external-auth.provider';

@Component({
  selector: 'app-profile',
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule],
  templateUrl: './profile.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./profile.component.scss']
})
export class ProfileComponent implements OnInit {
  private fb: FormBuilder = inject(FormBuilder);
  private authService: AuthService = inject(AuthService);
  private externalAuth = inject(ExternalAuthProvider);
  private userPreferencesService: UserPreferencesService = inject(UserPreferencesService);
  private modelService: ModelService = inject(ModelService);
  private personalityService: PersonalityService = inject(PersonalityService);
  private themeService: ThemeService = inject(ThemeService);
  private router: Router = inject(Router);
  private memoryService: MemoryService = inject(MemoryService);
  private readonly destroyRef = inject(DestroyRef);
  private renderer: Renderer2 = inject(Renderer2);

  profileForm: FormGroup;
  passwordForm: FormGroup;
  preferencesForm: FormGroup;
  isLoading = signal(false);
  isPasswordLoading = signal(false);
  isPreferencesLoading = signal(false);
  errorMessage = signal<string | null>(null);
  passwordErrorMessage = signal<string | null>(null);
  preferencesErrorMessage = signal<string | null>(null);
  successMessage = signal<string | null>(null);
  passwordSuccessMessage = signal<string | null>(null);
  preferencesSuccessMessage = signal<string | null>(null);
  currentUser = signal<UserResponse | null>(null);
  currentPreferences = signal<UserPreferences | null>(null);
  models = signal<Model[]>([]);
  personalities = signal<Personality[]>([]);
  /** IANA timezone identifiers for the timezone selector */
  timezones = signal<string[]>([]);
  /** Timezone combobox open state */
  timezoneComboboxOpen = signal(false);
  /** Search query for filtering timezones */
  timezoneSearchQuery = signal('');
  /** Index of highlighted option for keyboard nav */
  timezoneHighlightedIndex = signal(0);
  /** Filtered timezones for combobox list (max 100 shown) */
  filteredTimezones = computed(() => {
    const query = this.timezoneSearchQuery().toLowerCase().trim();
    const list = this.timezones();
    if (!query) return list.slice(0, 100);
    const filtered = list.filter(tz => tz.toLowerCase().includes(query));
    return filtered.slice(0, 100);
  });
  /** Whether there are more matches than shown */
  timezoneHasMoreMatches = computed(() => {
    const query = this.timezoneSearchQuery().toLowerCase().trim();
    const list = this.timezones();
    const filtered = query ? list.filter(tz => tz.toLowerCase().includes(query)) : list;
    return filtered.length > 100;
  });

  @ViewChild('timezoneCombobox') timezoneComboboxRef?: ElementRef<HTMLElement>;
  @ViewChild('memoryImportInput') memoryImportInputRef?: ElementRef<HTMLInputElement>;
  isExportingMemories = signal(false);
  isImportingMemories = signal(false);
  exportError = signal<string | null>(null);
  importError = signal<string | null>(null);
  importSuccess = signal<string | null>(null);

  constructor() {
    this.profileForm = this.fb.group({
      email: ['', [Validators.required, Validators.email, Validators.maxLength(255)]],
      first_name: ['', [Validators.maxLength(50)]],
      last_name: ['', [Validators.maxLength(50)]],
      timezone: ['', [Validators.maxLength(64)]]
    });

    this.passwordForm = this.fb.group({
      current_password: ['', [Validators.required]],
      new_password: ['', [Validators.required, Validators.minLength(8)]],
      confirm_password: ['', [Validators.required]]
    }, { validators: this.passwordMatchValidator });

    this.preferencesForm = this.fb.group({
      default_model_id: ['', [Validators.required]],
      default_personality_id: [''],
      theme: ['']
    });
  }

  ngOnInit(): void {
    this.loadTimezones();
    this.loadUserProfile();
    this.loadUserPreferences();
    this.loadModels();
    this.loadPersonalities();
  }

  private loadTimezones(): void {
    try {
      const tz = typeof Intl !== 'undefined' && Intl.supportedValuesOf
        ? Intl.supportedValuesOf('timeZone')
        : ['America/New_York', 'America/Los_Angeles', 'America/Chicago', 'Europe/London', 'Europe/Paris', 'Asia/Tokyo', 'UTC'];
      this.timezones.set(Array.isArray(tz) ? [...tz].sort() : ['America/New_York']);
    } catch {
      this.timezones.set(['America/New_York', 'America/Los_Angeles', 'UTC']);
    }
  }

  getTimezoneDisplayValue(): string {
    if (this.timezoneComboboxOpen()) {
      return this.timezoneSearchQuery();
    }
    return this.profileForm.get('timezone')?.value ?? '';
  }

  openTimezoneCombobox(): void {
    this.timezoneComboboxOpen.set(true);
    this.timezoneSearchQuery.set(this.profileForm.get('timezone')?.value ?? '');
    this.timezoneHighlightedIndex.set(0);
  }

  closeTimezoneCombobox(): void {
    this.timezoneComboboxOpen.set(false);
    this.timezoneSearchQuery.set('');
    this.timezoneHighlightedIndex.set(0);
  }

  selectTimezone(tz: string): void {
    this.profileForm.patchValue({ timezone: tz });
    this.profileForm.markAsDirty();
    this.closeTimezoneCombobox();
  }

  onTimezoneSearchInput(value: string): void {
    this.timezoneSearchQuery.set(value);
    this.timezoneHighlightedIndex.set(0);
  }

  onTimezoneInputKeydown(event: KeyboardEvent): void {
    const open = this.timezoneComboboxOpen();
    const filtered = this.filteredTimezones();
    const n = filtered.length;

    if (event.key === 'Escape') {
      this.closeTimezoneCombobox();
      event.preventDefault();
      return;
    }

    if (!open) return;

    if (event.key === 'Enter') {
      event.preventDefault();
      const idx = this.timezoneHighlightedIndex();
      if (n > 0 && filtered[idx] != null) {
        this.selectTimezone(filtered[idx]);
      }
      return;
    }

    if (event.key === 'ArrowDown') {
      event.preventDefault();
      this.timezoneHighlightedIndex.set(n === 0 ? 0 : (this.timezoneHighlightedIndex() + 1) % n);
      this.scrollTimezoneOptionIntoView();
      return;
    }

    if (event.key === 'ArrowUp') {
      event.preventDefault();
      this.timezoneHighlightedIndex.set(n === 0 ? 0 : (this.timezoneHighlightedIndex() - 1 + n) % n);
      this.scrollTimezoneOptionIntoView();
    }
  }

  private scrollTimezoneOptionIntoView(): void {
    setTimeout(() => {
      const id = 'timezone-option-' + this.timezoneHighlightedIndex();
      document.getElementById(id)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
    }, 0);
  }

  @HostListener('document:click', ['$event'])
  onDocumentClick(event: MouseEvent): void {
    const el = this.timezoneComboboxRef?.nativeElement;
    if (el && !el.contains(event.target as Node)) {
      this.closeTimezoneCombobox();
    }
  }

  private loadUserProfile(): void {
    this.isLoading.set(true);
    this.authService.getUserProfile().subscribe({
      next: (user) => {
        this.currentUser.set(user);
        this.profileForm.patchValue({
          email: user.email || '',
          first_name: user.first_name || '',
          last_name: user.last_name || '',
          timezone: user.timezone || ''
        });
        this.isLoading.set(false);
      },
      error: (error: any) => {
        this.isLoading.set(false);
        this.errorMessage.set(error.message || 'Failed to load profile. Please try again.');
      }
    });
  }

  private loadUserPreferences(): void {
    this.userPreferencesService.getUserPreferences().subscribe({
      next: (preferences) => {
        this.currentPreferences.set(preferences);
        // Use the user's selected mode (light/dark/system), not the resolved
        // value, so the dropdown reflects what they actually picked.
        const currentMode = this.themeService.mode();
        this.preferencesForm.patchValue({
          default_model_id: preferences.default_model_id || '',
          default_personality_id: preferences.default_personality_id || '',
          theme: currentMode || 'system'
        });
      },
      error: (error: any) => {
        console.error('Failed to load user preferences:', error);
        this.preferencesErrorMessage.set('Failed to load preferences. Please try again.');
        const currentMode = this.themeService.mode();
        this.preferencesForm.patchValue({
          theme: currentMode || 'system'
        });
      }
    });
  }

  private loadModels(): void {
    this.modelService.getModels().subscribe({
      next: (models) => {
        this.models.set(models);
      },
      error: (error: any) => {
        console.error('Failed to load models:', error);
        this.preferencesErrorMessage.set('Failed to load available models. Please try again.');
      }
    });
  }

  private loadPersonalities(): void {
    this.personalityService.listPersonalities().subscribe({
      next: (response) => {
        this.personalities.set(response.results);
      },
      error: (error: any) => {
        console.error('Failed to load personalities:', error);
        this.preferencesErrorMessage.set('Failed to load available personalities. Please try again.');
      }
    });
  }

  passwordMatchValidator(form: FormGroup) {
    const newPassword = form.get('new_password')?.value;
    const confirmPassword = form.get('confirm_password')?.value;

    if (newPassword && confirmPassword && newPassword !== confirmPassword) {
      form.get('confirm_password')?.setErrors({ passwordMismatch: true });
      return { passwordMismatch: true };
    }

    return null;
  }

  async onSubmit(): Promise<void> {
    if (this.profileForm.valid) {
      this.isLoading.set(true);
      this.errorMessage.set(null);
      this.successMessage.set(null);

      const firstNameValue = (this.profileForm.value.first_name ?? '').trim();
      const lastNameValue = (this.profileForm.value.last_name ?? '').trim();

      const timezoneValue = (this.profileForm.value.timezone ?? '').trim();
      const updateData: UpdateUserRequest = {
        email: (this.profileForm.value.email ?? '').trim(),
        first_name: firstNameValue || undefined,
        last_name: lastNameValue || undefined,
        timezone: timezoneValue || undefined
      };

      const isCognitoUser = await this.isExternalAuthenticated();

      if (isCognitoUser) {
        try {
          await this.syncExternalProfile(firstNameValue, lastNameValue);
        } catch (error: any) {
          this.isLoading.set(false);
          this.errorMessage.set(this.formatExternalProfileError(error));
          return;
        }
      }

      this.authService.updateProfile(updateData).subscribe({
        next: (user) => {
          this.isLoading.set(false);
          this.currentUser.set(user);
          this.successMessage.set('Profile updated successfully!');

          // Clear success message after 5 seconds
          setTimeout(() => {
            this.successMessage.set(null);
          }, 5000);
        },
        error: (error: any) => {
          this.isLoading.set(false);
          this.errorMessage.set(error.message || 'Failed to update profile. Please try again.');
        }
      });
    }
  }

  async onPasswordSubmit(): Promise<void> {
    if (this.passwordForm.valid) {
      this.isPasswordLoading.set(true);
      this.passwordErrorMessage.set(null);
      this.passwordSuccessMessage.set(null);

      const passwordData: UpdatePasswordRequest = {
        current_password: this.passwordForm.value.current_password,
        new_password: this.passwordForm.value.new_password
      };

      const isCognitoUser = await this.isExternalAuthenticated();

      if (isCognitoUser) {
        try {
          await this.externalAuth.updatePassword(
            passwordData.current_password,
            passwordData.new_password,
          );
          this.showPasswordSuccess();
        } catch (error: any) {
          this.isPasswordLoading.set(false);
          this.passwordErrorMessage.set(this.formatExternalPasswordError(error));
        }
        return;
      }

      this.authService.updatePassword(passwordData).subscribe({
        next: () => {
          this.showPasswordSuccess();
        },
        error: (error: any) => {
          this.isPasswordLoading.set(false);
          this.passwordErrorMessage.set(error.message || 'Failed to update password. Please try again.');
        }
      });
    }
  }

  onPreferencesSubmit(): void {
    if (this.preferencesForm.valid) {
      this.isPreferencesLoading.set(true);
      this.preferencesErrorMessage.set(null);
      this.preferencesSuccessMessage.set(null);

      const currentUser = this.currentUser();
      if (!currentUser) {
        this.preferencesErrorMessage.set('User information not available.');
        this.isPreferencesLoading.set(false);
        return;
      }

      const themeValue = this.preferencesForm.value.theme as ThemeMode;

      // Apply the new mode locally first so themeService.theme() resolves
      // 'system' against the OS preference before we sync the backend, which
      // only stores concrete 'light' | 'dark' values.
      if (themeValue) {
        this.themeService.setTheme(themeValue, false);
      }
      const preferencesData: UserPreferences = {
        user_id: currentUser.id,
        default_model_id: this.preferencesForm.value.default_model_id,
        default_personality_id: this.preferencesForm.value.default_personality_id || undefined,
        theme: themeValue
      };

      this.userPreferencesService.updateUserPreferences(preferencesData).subscribe({
        next: (preferences) => {
          this.isPreferencesLoading.set(false);
          this.currentPreferences.set(preferences);

          this.preferencesSuccessMessage.set('Chat preferences updated successfully!');

          // Clear success message after 5 seconds
          setTimeout(() => {
            this.preferencesSuccessMessage.set(null);
          }, 5000);
        },
        error: (error: any) => {
          this.isPreferencesLoading.set(false);
          this.preferencesErrorMessage.set(error.message || 'Failed to update preferences. Please try again.');
        }
      });
    }
  }

  navigateToDashboard(): void {
    this.router.navigate(['/dashboard']);
  }

  exportMemories(): void {
    this.isExportingMemories.set(true);
    this.exportError.set(null);
    this.memoryService.exportMemories().pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: (blob) => {
        const url = URL.createObjectURL(blob);
        const a = this.renderer.createElement('a') as HTMLAnchorElement;
        a.href = url;
        a.download = 'memories.zip'; // server Content-Disposition takes precedence
        this.renderer.appendChild(document.body, a);
        a.click();
        this.renderer.removeChild(document.body, a);
        URL.revokeObjectURL(url);
        this.isExportingMemories.set(false);
      },
      error: (err) => {
        this.exportError.set('Failed to export memories. Please try again.');
        this.isExportingMemories.set(false);
      }
    });
  }

  openMemoryImportFilePicker(): void {
    this.importError.set(null);
    this.importSuccess.set(null);
    this.memoryImportInputRef?.nativeElement.click();
  }

  onMemoryImportFileSelected(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) {
      return;
    }

    this.isImportingMemories.set(true);
    this.importError.set(null);
    this.importSuccess.set(null);

    this.memoryService.importMemories(file).pipe(takeUntilDestroyed(this.destroyRef)).subscribe({
      next: (result: MemoryImportResult) => {
        this.isImportingMemories.set(false);
        this.importSuccess.set(this.formatImportSuccess(result));
        input.value = '';
      },
      error: () => {
        this.isImportingMemories.set(false);
        this.importError.set('Failed to import memories. Please verify this is a valid export ZIP and try again.');
        input.value = '';
      }
    });
  }

  private formatImportSuccess(result: MemoryImportResult): string {
    return `Imported ${result.imported_count} memories (${result.duplicate_count} duplicates skipped, ${result.invalid_record_count} invalid records skipped, ${result.skipped_missing_chat_count} missing chats skipped, ${result.skipped_missing_personality_count} missing personalities skipped).`;
  }

  getDisplayName(): string {
    const user = this.currentUser();
    if (!user) return '';

    const firstName = user.first_name || '';
    const lastName = user.last_name || '';

    if (firstName && lastName) {
      return `${firstName} ${lastName}`;
    } else if (firstName) {
      return firstName;
    } else if (lastName) {
      return lastName;
    } else {
      return user.username;
    }
  }

  getUserInitials(): string {
    const user = this.currentUser();
    if (!user) return '';

    const firstName = user.first_name || '';
    const lastName = user.last_name || '';

    if (firstName && lastName) {
      return `${firstName[0].toUpperCase()}${lastName[0].toUpperCase()}`;
    } else if (firstName) {
      return firstName[0].toUpperCase();
    } else if (lastName) {
      return lastName[0].toUpperCase();
    } else {
      return user.username[0].toUpperCase();
    }
  }

  getModelDisplayName(modelId?: string): string {
    if (!modelId) return 'Default Model';
    const model = this.models().find(m => m.id === modelId);
    return model?.display_name || 'Unknown Model';
  }

  trackByModelId(index: number, model: Model): string {
    return model.id;
  }

  getPersonalitiesWithDefault(): Personality[] {
    return this.personalities();
  }

  getPersonalityDisplayName(personalityId?: string): string {
    if (!personalityId || personalityId === NULL_PERSONALITY_ID) return 'None';
    const personality = this.personalities().find(p => p.id === personalityId);
    return personality?.name || 'Unknown Personality';
  }

  trackByPersonalityId(index: number, personality: Personality): string {
    return personality.id;
  }

  private async isExternalAuthenticated(): Promise<boolean> {
    if (!this.externalAuth.available) {
      return false;
    }
    try {
      const tokens = await this.externalAuth.fetchSession();
      return Boolean(tokens?.accessToken);
    } catch {
      return false;
    }
  }

  private showPasswordSuccess(): void {
    this.isPasswordLoading.set(false);
    this.passwordSuccessMessage.set('Password updated successfully!');
    this.passwordForm.reset();
    setTimeout(() => {
      this.passwordSuccessMessage.set(null);
    }, 5000);
  }

  private formatExternalPasswordError(error: any): string {
    if (typeof error?.message === 'string' && error.message.trim().length > 0) {
      return error.message;
    }
    return 'Failed to update password. Please try again.';
  }

  private async syncExternalProfile(firstName: string, lastName: string): Promise<void> {
    const attributes: Record<string, string> = {};
    if (firstName) {
      attributes['given_name'] = firstName;
    }
    if (lastName) {
      attributes['family_name'] = lastName;
    }

    if (Object.keys(attributes).length === 0) {
      return;
    }

    await this.externalAuth.updateProfileAttributes(attributes);
  }

  logout(): void {
    void this.authService.logoutPreferred();
  }

  private formatExternalProfileError(error: any): string {
    if (typeof error?.message === 'string' && error.message.trim().length > 0) {
      return error.message;
    }
    return 'Failed to update profile. Please try again.';
  }
}

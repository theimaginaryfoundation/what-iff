import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { Router, ActivatedRoute } from '@angular/router';
import { of, throwError } from 'rxjs';
import { ProfileComponent } from './profile.component';
import { AuthService } from '../../core/services/auth.service';
import {
    ExternalAuthProvider,
    NoopExternalAuthProvider,
} from '../../core/auth/external-auth.provider';
import { UserPreferencesService } from '../../core/services/user-preferences.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { UserResponse, UserPreferences } from '../../core/models/user.model';
import { Model } from '../../core/models/model.model';
import { Personality } from '../../core/models/personality.model';
import { NULL_PERSONALITY_ID } from '../../core/constants/app.constants';

describe('ProfileComponent', () => {
    let component: ProfileComponent;
    let fixture: ComponentFixture<ProfileComponent>;
    let mockAuthService: Pick<MockedObject<AuthService>, 'getUserProfile' | 'updateProfile' | 'updatePassword' | 'currentUser' | 'isLoggedIn'>;
    let mockUserPreferencesService: Pick<MockedObject<UserPreferencesService>, 'getUserPreferences' | 'updateUserPreferences'>;
    let mockModelService: Pick<MockedObject<ModelService>, 'getModels'>;
    let mockPersonalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
    let mockRouter: Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
    let mockActivatedRoute: any;

    const mockUser: UserResponse = {
        id: 'user-123',
        username: 'testuser',
        email: 'test@example.com',
        first_name: 'Test',
        last_name: 'User',
        timezone: 'America/New_York',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z'
    };

    const mockPreferences: UserPreferences = {
        user_id: 'user-123',
        default_model_id: 'model-1',
        default_personality_id: 'personality-1'
    };

    const mockModels: Model[] = [
        {
            id: 'model-1',
            name: 'GPT-4',
            display_name: 'GPT-4',
            description: 'Most capable model',
            tool_support: true
        },
        {
            id: 'model-2',
            name: 'GPT-3.5 Turbo',
            display_name: 'GPT-3.5 Turbo',
            description: 'Fast and efficient',
            tool_support: false
        }
    ];

    const mockPersonalities: Personality[] = [
        {
            id: 'personality-1',
            name: 'Helpful Assistant',
            system_prompt: 'You are a helpful assistant',
            auto_pin_memories: false,
            expressions_enabled: true,
            image_style: 'auto', cover_image_id: null,
            cover_image_url: null,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            stats: { chat_count: 0, last_used_at: null }
        },
        {
            id: 'personality-2',
            name: 'Professional Writer',
            system_prompt: 'You are a professional writer',
            auto_pin_memories: false,
            expressions_enabled: true,
            image_style: 'auto', cover_image_id: null,
            cover_image_url: null,
            created_at: '2024-01-01T00:00:00Z',
            updated_at: '2024-01-01T00:00:00Z',
            stats: { chat_count: 0, last_used_at: null }
        }
    ];

    beforeEach(async () => {
        // ProfileComponent injects the real ThemeService, whose constructor reads
        // localStorage. Other specs in the suite may leave a value behind, so we
        // start from a known-empty state to keep the form's theme assertions
        // deterministic.
        localStorage.clear();

        // Create mocks
        mockAuthService = {
            getUserProfile: vi.fn().mockName("AuthService.getUserProfile"),
            updateProfile: vi.fn().mockName("AuthService.updateProfile"),
            updatePassword: vi.fn().mockName("AuthService.updatePassword"),
            currentUser: signal<UserResponse | null>(mockUser), isLoggedIn: signal(true)
        } as unknown as Pick<MockedObject<AuthService>, 'getUserProfile' | 'updateProfile' | 'updatePassword' | 'currentUser' | 'isLoggedIn'>;
        mockUserPreferencesService = {
            getUserPreferences: vi.fn().mockName("UserPreferencesService.getUserPreferences"),
            updateUserPreferences: vi.fn().mockName("UserPreferencesService.updateUserPreferences")
        } as unknown as Pick<MockedObject<UserPreferencesService>, 'getUserPreferences' | 'updateUserPreferences'>;
        mockModelService = {
            getModels: vi.fn().mockName("ModelService.getModels")
        } as unknown as Pick<MockedObject<ModelService>, 'getModels'>;
        mockPersonalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities'>;
        mockRouter = {
            navigate: vi.fn().mockName("Router.navigate"),
            createUrlTree: vi.fn().mockName("Router.createUrlTree"),
            serializeUrl: vi.fn().mockName("Router.serializeUrl"),
            events: of()
        } as unknown as Pick<MockedObject<Router>, 'navigate' | 'createUrlTree' | 'serializeUrl' | 'events'>;
        mockRouter.createUrlTree.mockReturnValue({} as any);
        mockRouter.serializeUrl.mockReturnValue('');

        mockActivatedRoute = {
            snapshot: {
                paramMap: {
                    get: vi.fn().mockName('get')
                }
            },
            params: of({}),
            queryParams: of({})
        };

        // Configure TestBed
        await TestBed.configureTestingModule({
            imports: [ProfileComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                { provide: AuthService, useValue: mockAuthService },
                { provide: ExternalAuthProvider, useClass: NoopExternalAuthProvider },
                { provide: UserPreferencesService, useValue: mockUserPreferencesService },
                { provide: ModelService, useValue: mockModelService },
                { provide: PersonalityService, useValue: mockPersonalityService },
                { provide: Router, useValue: mockRouter },
                { provide: ActivatedRoute, useValue: mockActivatedRoute }
            ]
        }).compileComponents();

        // Set default return values
        mockAuthService.getUserProfile.mockReturnValue(of(mockUser));
        mockUserPreferencesService.getUserPreferences.mockReturnValue(of(mockPreferences));
        mockModelService.getModels.mockReturnValue(of(mockModels));
        mockPersonalityService.listPersonalities.mockReturnValue(of({
            results: mockPersonalities,
            total_count: 2,
            page: 1,
            page_size: 10
        }));

        // Create component
        fixture = TestBed.createComponent(ProfileComponent);
        component = fixture.componentInstance;
    });

    afterEach(() => {
        // Clean up side effects
        document.body.style.overflow = '';
        localStorage.clear();
    });

    it('should create', () => {
        expect(component).toBeTruthy();
    });

    describe('ngOnInit', () => {
        it('should load user profile, preferences, models, and personalities', () => {
            fixture.detectChanges();

            expect(mockAuthService.getUserProfile).toHaveBeenCalled();
            expect(mockUserPreferencesService.getUserPreferences).toHaveBeenCalled();
            expect(mockModelService.getModels).toHaveBeenCalled();
            expect(mockPersonalityService.listPersonalities).toHaveBeenCalled();
            expect(component.currentUser()).toEqual(mockUser);
            expect(component.currentPreferences()).toEqual(mockPreferences);
            expect(component.models()).toEqual(mockModels);
            expect(component.personalities()).toEqual(mockPersonalities);
        });

        it('should populate profile form with user data', () => {
            fixture.detectChanges();

            expect(component.profileForm.value).toEqual({
                email: 'test@example.com',
                first_name: 'Test',
                last_name: 'User',
                timezone: 'America/New_York'
            });
        });

        it('should populate preferences form with user preferences', () => {
            fixture.detectChanges();

            // The theme dropdown reflects ThemeService.mode() so users can see
            // when they're on `system` (added in Phase 01) vs an explicit choice.
            expect(component.preferencesForm.value).toEqual({
                default_model_id: 'model-1',
                default_personality_id: 'personality-1',
                theme: 'system'
            });
        });

        it('accepts `system` as a valid theme form value', () => {
            fixture.detectChanges();

            component.preferencesForm.patchValue({ theme: 'system' });

            expect(component.preferencesForm.get('theme')?.valid).toBe(true);
            expect(component.preferencesForm.value.theme).toBe('system');
        });

        it('should set loading state while fetching user profile', () => {
            expect(component.isLoading()).toBe(false);

            fixture.detectChanges();

            expect(component.isLoading()).toBe(false); // Should be false after loading completes
        });

        it('should handle error when loading user profile fails', async () => {
            const error = { message: 'Failed to load profile. Please try again.' };
            mockAuthService.getUserProfile.mockReturnValue(throwError(() => error));

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.isLoading()).toBe(false);
            expect(component.errorMessage()).toBe('Failed to load profile. Please try again.');
        });

        it('should handle error when loading preferences fails', async () => {
            const error = new Error('Failed to load preferences');
            mockUserPreferencesService.getUserPreferences.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(console.error).toHaveBeenCalledWith('Failed to load user preferences:', error);
            expect(component.preferencesErrorMessage()).toBe('Failed to load preferences. Please try again.');
        });

        it('should handle error when loading models fails', async () => {
            const error = new Error('Failed to load models');
            mockModelService.getModels.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(console.error).toHaveBeenCalledWith('Failed to load models:', error);
            expect(component.preferencesErrorMessage()).toBe('Failed to load available models. Please try again.');
        });

        it('should handle error when loading personalities fails', async () => {
            const error = new Error('Failed to load personalities');
            mockPersonalityService.listPersonalities.mockReturnValue(throwError(() => error));
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            fixture.detectChanges();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(console.error).toHaveBeenCalledWith('Failed to load personalities:', error);
            expect(component.preferencesErrorMessage()).toBe('Failed to load available personalities. Please try again.');
        });
    });

    describe('profile form', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        describe('validation', () => {
            it('should require email', () => {
                const emailControl = component.profileForm.get('email');
                emailControl?.setValue('');

                expect(emailControl?.hasError('required')).toBe(true);
                expect(component.profileForm.invalid).toBe(true);
            });

            it('should validate email format', () => {
                const emailControl = component.profileForm.get('email');
                emailControl?.setValue('invalid-email');

                expect(emailControl?.hasError('email')).toBe(true);
                expect(component.profileForm.invalid).toBe(true);
            });

            it('should validate email max length', () => {
                const emailControl = component.profileForm.get('email');
                emailControl?.setValue('a'.repeat(256) + '@example.com');

                expect(emailControl?.hasError('maxlength')).toBe(true);
                expect(component.profileForm.invalid).toBe(true);
            });

            it('should validate first name max length', () => {
                const firstNameControl = component.profileForm.get('first_name');
                firstNameControl?.setValue('a'.repeat(51));

                expect(firstNameControl?.hasError('maxlength')).toBe(true);
                expect(component.profileForm.invalid).toBe(true);
            });

            it('should validate last name max length', () => {
                const lastNameControl = component.profileForm.get('last_name');
                lastNameControl?.setValue('a'.repeat(51));

                expect(lastNameControl?.hasError('maxlength')).toBe(true);
                expect(component.profileForm.invalid).toBe(true);
            });

            it('should validate timezone max length', () => {
                const timezoneControl = component.profileForm.get('timezone');
                timezoneControl?.setValue('a'.repeat(65));

                expect(timezoneControl?.hasError('maxlength')).toBe(true);
                expect(component.profileForm.invalid).toBe(true);
            });

            it('should be valid with required fields', () => {
                component.profileForm.patchValue({
                    email: 'test@example.com',
                    first_name: 'Test',
                    last_name: 'User',
                    timezone: 'America/New_York'
                });

                expect(component.profileForm.valid).toBe(true);
            });

            it('should be valid without optional fields', () => {
                component.profileForm.patchValue({
                    email: 'test@example.com',
                    first_name: '',
                    last_name: '',
                    timezone: ''
                });

                expect(component.profileForm.valid).toBe(true);
            });
        });

        describe('onSubmit', () => {
            it('should update profile successfully', async () => {
                const updatedUser = { ...mockUser, first_name: 'Updated' };
                mockAuthService.updateProfile.mockReturnValue(of(updatedUser));

                component.profileForm.patchValue({
                    email: 'test@example.com',
                    first_name: 'Updated',
                    last_name: 'User',
                    timezone: 'America/New_York'
                });
                component.profileForm.markAsDirty();

                component.onSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(mockAuthService.updateProfile).toHaveBeenCalledWith({
                    email: 'test@example.com',
                    first_name: 'Updated',
                    last_name: 'User',
                    timezone: 'America/New_York'
                });
                expect(component.currentUser()).toEqual(updatedUser);
                expect(component.successMessage()).toBe('Profile updated successfully!');
                expect(component.isLoading()).toBe(false);
            });

            it('should update profile with undefined for empty optional fields', async () => {
                mockAuthService.updateProfile.mockReturnValue(of(mockUser));

                component.profileForm.patchValue({
                    email: 'test@example.com',
                    first_name: '',
                    last_name: '',
                    timezone: ''
                });
                component.profileForm.markAsDirty();

                component.onSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(mockAuthService.updateProfile).toHaveBeenCalledWith({
                    email: 'test@example.com',
                    first_name: undefined,
                    last_name: undefined,
                    timezone: undefined
                });
            });

            it('should handle update error', async () => {
                const error = { message: 'Failed to update profile. Please try again.' };
                mockAuthService.updateProfile.mockReturnValue(throwError(() => error));

                component.profileForm.patchValue({
                    email: 'test@example.com',
                    first_name: 'Test',
                    last_name: 'User',
                    timezone: 'America/New_York'
                });
                component.profileForm.markAsDirty();

                component.onSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(component.isLoading()).toBe(false);
                expect(component.errorMessage()).toBe('Failed to update profile. Please try again.');
                expect(component.successMessage()).toBeNull();
            });

            it('should not submit if form is invalid', () => {
                component.profileForm.patchValue({
                    email: 'invalid-email',
                    first_name: 'Test',
                    last_name: 'User'
                });

                component.onSubmit();

                expect(mockAuthService.updateProfile).not.toHaveBeenCalled();
            });

            it('should clear error and success messages on submit', async () => {
                mockAuthService.updateProfile.mockReturnValue(of(mockUser));
                component.errorMessage.set('Previous error');
                component.successMessage.set('Previous success');

                component.profileForm.patchValue({
                    email: 'test@example.com',
                    first_name: 'Test',
                    last_name: 'User',
                    timezone: 'America/New_York'
                });
                component.profileForm.markAsDirty();

                component.onSubmit();

                // Check immediately after calling submit, before async completion
                expect(component.errorMessage()).toBeNull();

                // Wait for async to complete to avoid side effects
                await new Promise(resolve => setTimeout(resolve, 0));
            });
        });
    });

    describe('password form', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        describe('validation', () => {
            it('should require current password', () => {
                const currentPasswordControl = component.passwordForm.get('current_password');
                currentPasswordControl?.setValue('');

                expect(currentPasswordControl?.hasError('required')).toBe(true);
                expect(component.passwordForm.invalid).toBe(true);
            });

            it('should require new password', () => {
                const newPasswordControl = component.passwordForm.get('new_password');
                newPasswordControl?.setValue('');

                expect(newPasswordControl?.hasError('required')).toBe(true);
                expect(component.passwordForm.invalid).toBe(true);
            });

            it('should validate new password minimum length', () => {
                const newPasswordControl = component.passwordForm.get('new_password');
                newPasswordControl?.setValue('short');

                expect(newPasswordControl?.hasError('minlength')).toBe(true);
                expect(component.passwordForm.invalid).toBe(true);
            });

            it('should require confirm password', () => {
                const confirmPasswordControl = component.passwordForm.get('confirm_password');
                confirmPasswordControl?.setValue('');

                expect(confirmPasswordControl?.hasError('required')).toBe(true);
                expect(component.passwordForm.invalid).toBe(true);
            });

            it('should validate password match', () => {
                component.passwordForm.patchValue({
                    current_password: 'oldpassword',
                    new_password: 'newpassword123',
                    confirm_password: 'differentpassword'
                });

                expect(component.passwordForm.hasError('passwordMismatch')).toBe(true);
                expect(component.passwordForm.get('confirm_password')?.hasError('passwordMismatch')).toBe(true);
            });

            it('should be valid when passwords match', () => {
                component.passwordForm.patchValue({
                    current_password: 'oldpassword',
                    new_password: 'newpassword123',
                    confirm_password: 'newpassword123'
                });

                expect(component.passwordForm.valid).toBe(true);
                expect(component.passwordForm.hasError('passwordMismatch')).toBe(false);
            });
        });

        describe('onPasswordSubmit', () => {
            it('should update password successfully', async () => {
                mockAuthService.updatePassword.mockReturnValue(of({ message: 'Password updated' }));

                component.passwordForm.patchValue({
                    current_password: 'oldpassword',
                    new_password: 'newpassword123',
                    confirm_password: 'newpassword123'
                });

                component.onPasswordSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(mockAuthService.updatePassword).toHaveBeenCalledWith({
                    current_password: 'oldpassword',
                    new_password: 'newpassword123'
                });
                expect(component.passwordSuccessMessage()).toBe('Password updated successfully!');
                expect(component.isPasswordLoading()).toBe(false);
            });

            it('should reset password form after successful update', async () => {
                mockAuthService.updatePassword.mockReturnValue(of({ message: 'Password updated' }));

                component.passwordForm.patchValue({
                    current_password: 'oldpassword',
                    new_password: 'newpassword123',
                    confirm_password: 'newpassword123'
                });

                component.onPasswordSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(component.passwordForm.value).toEqual({
                    current_password: null,
                    new_password: null,
                    confirm_password: null
                });
            });

            it('should handle password update error', async () => {
                const error = { message: 'Failed to update password. Please try again.' };
                mockAuthService.updatePassword.mockReturnValue(throwError(() => error));

                component.passwordForm.patchValue({
                    current_password: 'wrongpassword',
                    new_password: 'newpassword123',
                    confirm_password: 'newpassword123'
                });

                component.onPasswordSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(component.isPasswordLoading()).toBe(false);
                expect(component.passwordErrorMessage()).toBe('Failed to update password. Please try again.');
                expect(component.passwordSuccessMessage()).toBeNull();
            });

            it('should not submit if form is invalid', () => {
                component.passwordForm.patchValue({
                    current_password: 'oldpassword',
                    new_password: 'short',
                    confirm_password: 'short'
                });

                component.onPasswordSubmit();

                expect(mockAuthService.updatePassword).not.toHaveBeenCalled();
            });

            it('should clear error and success messages on submit', async () => {
                mockAuthService.updatePassword.mockReturnValue(of({ message: 'Password updated' }));
                component.passwordErrorMessage.set('Previous error');
                component.passwordSuccessMessage.set('Previous success');

                component.passwordForm.patchValue({
                    current_password: 'oldpassword',
                    new_password: 'newpassword123',
                    confirm_password: 'newpassword123'
                });

                component.onPasswordSubmit();

                // Check immediately after calling submit, before async completion
                expect(component.passwordErrorMessage()).toBeNull();

                // Wait for async to complete to avoid side effects
                await new Promise(resolve => setTimeout(resolve, 0));
            });
        });

        describe('passwordMatchValidator', () => {
            it('should return null when passwords match', () => {
                component.passwordForm.patchValue({
                    new_password: 'password123',
                    confirm_password: 'password123'
                });

                const result = component.passwordMatchValidator(component.passwordForm);

                expect(result).toBeNull();
            });

            it('should return error when passwords do not match', () => {
                component.passwordForm.patchValue({
                    new_password: 'password123',
                    confirm_password: 'different'
                });

                const result = component.passwordMatchValidator(component.passwordForm);

                expect(result).toEqual({ passwordMismatch: true });
            });

            it('should return null when passwords are empty', () => {
                component.passwordForm.patchValue({
                    new_password: '',
                    confirm_password: ''
                });

                const result = component.passwordMatchValidator(component.passwordForm);

                expect(result).toBeNull();
            });
        });
    });

    describe('preferences form', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        describe('validation', () => {
            it('should require default model', () => {
                const modelControl = component.preferencesForm.get('default_model_id');
                modelControl?.setValue('');

                expect(modelControl?.hasError('required')).toBe(true);
                expect(component.preferencesForm.invalid).toBe(true);
            });

            it('should not require default personality', () => {
                component.preferencesForm.patchValue({
                    default_model_id: 'model-1',
                    default_personality_id: ''
                });

                expect(component.preferencesForm.valid).toBe(true);
            });

            it('should be valid with both fields filled', () => {
                component.preferencesForm.patchValue({
                    default_model_id: 'model-1',
                    default_personality_id: 'personality-1'
                });

                expect(component.preferencesForm.valid).toBe(true);
            });
        });

        describe('onPreferencesSubmit', () => {
            beforeEach(() => {
                component.currentUser.set(mockUser);
            });

            it('should update preferences successfully', async () => {
                const updatedPreferences: UserPreferences = {
                    user_id: 'user-123',
                    default_model_id: 'model-2',
                    default_personality_id: 'personality-2',
                    theme: 'light' as 'light' | 'dark'
                };
                mockUserPreferencesService.updateUserPreferences.mockReturnValue(of(updatedPreferences));

                component.preferencesForm.patchValue({
                    default_model_id: 'model-2',
                    default_personality_id: 'personality-2',
                    theme: 'light'
                });
                component.preferencesForm.markAsDirty();

                component.onPreferencesSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(mockUserPreferencesService.updateUserPreferences).toHaveBeenCalledWith({
                    user_id: 'user-123',
                    default_model_id: 'model-2',
                    default_personality_id: 'personality-2',
                    theme: 'light'
                });
                expect(component.currentPreferences()).toEqual(updatedPreferences);
                expect(component.preferencesSuccessMessage()).toBe('Chat preferences updated successfully!');
                expect(component.isPreferencesLoading()).toBe(false);
            });

            it('should update preferences with undefined personality', async () => {
                const updatedPreferences: UserPreferences = {
                    user_id: 'user-123',
                    default_model_id: 'model-2',
                    default_personality_id: undefined,
                    theme: 'light' as 'light' | 'dark'
                };
                mockUserPreferencesService.updateUserPreferences.mockReturnValue(of(updatedPreferences));

                component.preferencesForm.patchValue({
                    default_model_id: 'model-2',
                    default_personality_id: '',
                    theme: 'light'
                });
                component.preferencesForm.markAsDirty();

                component.onPreferencesSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(mockUserPreferencesService.updateUserPreferences).toHaveBeenCalledWith({
                    user_id: 'user-123',
                    default_model_id: 'model-2',
                    default_personality_id: undefined,
                    theme: 'light'
                });
            });

            it('should handle preferences update error', async () => {
                const error = { message: 'Failed to update preferences. Please try again.' };
                mockUserPreferencesService.updateUserPreferences.mockReturnValue(throwError(() => error));

                component.preferencesForm.patchValue({
                    default_model_id: 'model-2',
                    default_personality_id: 'personality-2'
                });
                component.preferencesForm.markAsDirty();

                component.onPreferencesSubmit();

                await new Promise(resolve => setTimeout(resolve, 0));

                expect(component.isPreferencesLoading()).toBe(false);
                expect(component.preferencesErrorMessage()).toBe('Failed to update preferences. Please try again.');
                expect(component.preferencesSuccessMessage()).toBeNull();
            });

            it('should not submit if form is invalid', () => {
                component.preferencesForm.patchValue({
                    default_model_id: '',
                    default_personality_id: 'personality-1'
                });

                component.onPreferencesSubmit();

                expect(mockUserPreferencesService.updateUserPreferences).not.toHaveBeenCalled();
            });

            it('should handle missing user information', () => {
                component.currentUser.set(null);

                component.preferencesForm.patchValue({
                    default_model_id: 'model-1',
                    default_personality_id: 'personality-1'
                });

                component.onPreferencesSubmit();

                expect(component.preferencesErrorMessage()).toBe('User information not available.');
                expect(component.isPreferencesLoading()).toBe(false);
                expect(mockUserPreferencesService.updateUserPreferences).not.toHaveBeenCalled();
            });

            it('should clear error and success messages on submit', async () => {
                mockUserPreferencesService.updateUserPreferences.mockReturnValue(of(mockPreferences));
                component.preferencesErrorMessage.set('Previous error');
                component.preferencesSuccessMessage.set('Previous success');

                component.preferencesForm.patchValue({
                    default_model_id: 'model-1',
                    default_personality_id: 'personality-1'
                });
                component.preferencesForm.markAsDirty();

                component.onPreferencesSubmit();

                // Check immediately after calling submit, before async completion
                expect(component.preferencesErrorMessage()).toBeNull();

                // Wait for async to complete to avoid side effects
                await new Promise(resolve => setTimeout(resolve, 0));
            });
        });
    });

    describe('navigation', () => {
        it('should navigate to dashboard', () => {
            component.navigateToDashboard();

            expect(mockRouter.navigate).toHaveBeenCalledWith(['/dashboard']);
        });
    });

    describe('helper methods', () => {
        beforeEach(() => {
            fixture.detectChanges();
        });

        describe('getDisplayName', () => {
            it('should return full name when both first and last names are available', () => {
                component.currentUser.set(mockUser);

                expect(component.getDisplayName()).toBe('Test User');
            });

            it('should return first name only when last name is missing', () => {
                const userWithFirstNameOnly = { ...mockUser, last_name: '' };
                component.currentUser.set(userWithFirstNameOnly);

                expect(component.getDisplayName()).toBe('Test');
            });

            it('should return last name only when first name is missing', () => {
                const userWithLastNameOnly = { ...mockUser, first_name: '' };
                component.currentUser.set(userWithLastNameOnly);

                expect(component.getDisplayName()).toBe('User');
            });

            it('should return username when both names are missing', () => {
                const userWithoutNames = { ...mockUser, first_name: '', last_name: '' };
                component.currentUser.set(userWithoutNames);

                expect(component.getDisplayName()).toBe('testuser');
            });

            it('should return empty string when user is null', () => {
                component.currentUser.set(null);

                expect(component.getDisplayName()).toBe('');
            });
        });

        describe('getUserInitials', () => {
            it('should return initials from first and last names', () => {
                component.currentUser.set(mockUser);

                expect(component.getUserInitials()).toBe('TU');
            });

            it('should return first letter of first name when last name is missing', () => {
                const userWithFirstNameOnly = { ...mockUser, last_name: '' };
                component.currentUser.set(userWithFirstNameOnly);

                expect(component.getUserInitials()).toBe('T');
            });

            it('should return first letter of last name when first name is missing', () => {
                const userWithLastNameOnly = { ...mockUser, first_name: '' };
                component.currentUser.set(userWithLastNameOnly);

                expect(component.getUserInitials()).toBe('U');
            });

            it('should return first letter of username when both names are missing', () => {
                const userWithoutNames = { ...mockUser, first_name: '', last_name: '' };
                component.currentUser.set(userWithoutNames);

                expect(component.getUserInitials()).toBe('T');
            });

            it('should return uppercase initials', () => {
                const userWithLowercaseNames = {
                    ...mockUser,
                    first_name: 'test',
                    last_name: 'user',
                    username: 'testuser'
                };
                component.currentUser.set(userWithLowercaseNames);

                expect(component.getUserInitials()).toBe('TU');
            });

            it('should return empty string when user is null', () => {
                component.currentUser.set(null);

                expect(component.getUserInitials()).toBe('');
            });
        });

        describe('getModelDisplayName', () => {
            it('should return model display name when model exists', () => {
                const displayName = component.getModelDisplayName('model-1');

                expect(displayName).toBe('GPT-4');
            });

            it('should return "Unknown Model" when model does not exist', () => {
                const displayName = component.getModelDisplayName('nonexistent-model');

                expect(displayName).toBe('Unknown Model');
            });

            it('should return "Default Model" when model ID is undefined', () => {
                const displayName = component.getModelDisplayName(undefined);

                expect(displayName).toBe('Default Model');
            });

            it('should return "Default Model" when model ID is empty string', () => {
                const displayName = component.getModelDisplayName('');

                expect(displayName).toBe('Default Model');
            });
        });

        describe('getPersonalitiesWithDefault', () => {
            it('should return the personalities list without a synthetic default entry', () => {
                const personalities = component.getPersonalitiesWithDefault();

                expect(personalities.length).toBe(2);
                expect(personalities[0]).toEqual(mockPersonalities[0]);
                expect(personalities[1]).toEqual(mockPersonalities[1]);
            });

            it('should not modify original personalities array', () => {
                const originalLength = component.personalities().length;

                component.getPersonalitiesWithDefault();

                expect(component.personalities().length).toBe(originalLength);
            });
        });

        describe('getPersonalityDisplayName', () => {
            it('should return personality name when personality exists', () => {
                const displayName = component.getPersonalityDisplayName('personality-1');

                expect(displayName).toBe('Helpful Assistant');
            });

            it('should return "None" for NULL_PERSONALITY_ID', () => {
                const displayName = component.getPersonalityDisplayName(NULL_PERSONALITY_ID);

                expect(displayName).toBe('None');
            });

            it('should return "Unknown Personality" when personality does not exist', () => {
                const displayName = component.getPersonalityDisplayName('nonexistent-personality');

                expect(displayName).toBe('Unknown Personality');
            });

            it('should return "None" when personality ID is undefined', () => {
                const displayName = component.getPersonalityDisplayName(undefined);

                expect(displayName).toBe('None');
            });

            it('should return "None" when personality ID is empty string', () => {
                const displayName = component.getPersonalityDisplayName('');

                expect(displayName).toBe('None');
            });
        });

        describe('trackByModelId', () => {
            it('should return model id', () => {
                const model = mockModels[0];

                expect(component.trackByModelId(0, model)).toBe('model-1');
            });
        });

        describe('trackByPersonalityId', () => {
            it('should return personality id', () => {
                const personality = mockPersonalities[0];

                expect(component.trackByPersonalityId(0, personality)).toBe('personality-1');
            });
        });
    });

    describe('loading states', () => {
        it('should initialize all loading states as false', () => {
            expect(component.isLoading()).toBe(false);
            expect(component.isPasswordLoading()).toBe(false);
            expect(component.isPreferencesLoading()).toBe(false);
        });
    });

    describe('message states', () => {
        it('should initialize all message states as null', () => {
            expect(component.errorMessage()).toBeNull();
            expect(component.passwordErrorMessage()).toBeNull();
            expect(component.preferencesErrorMessage()).toBeNull();
            expect(component.successMessage()).toBeNull();
            expect(component.passwordSuccessMessage()).toBeNull();
            expect(component.preferencesSuccessMessage()).toBeNull();
        });

        it('should set success message after successful profile update', async () => {
            mockAuthService.updateProfile.mockReturnValue(of(mockUser));
            fixture.detectChanges();

            component.profileForm.patchValue({
                email: 'test@example.com',
                first_name: 'Test',
                last_name: 'User',
                timezone: 'America/New_York'
            });
            component.profileForm.markAsDirty();

            component.onSubmit();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.successMessage()).toBe('Profile updated successfully!');
        });

        it('should set success message after successful password update', async () => {
            mockAuthService.updatePassword.mockReturnValue(of({ message: 'Password updated' }));
            fixture.detectChanges();

            component.passwordForm.patchValue({
                current_password: 'oldpassword',
                new_password: 'newpassword123',
                confirm_password: 'newpassword123'
            });

            component.onPasswordSubmit();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.passwordSuccessMessage()).toBe('Password updated successfully!');
        });

        it('should set success message after successful preferences update', async () => {
            mockUserPreferencesService.updateUserPreferences.mockReturnValue(of(mockPreferences));
            fixture.detectChanges();

            component.preferencesForm.patchValue({
                default_model_id: 'model-1',
                default_personality_id: 'personality-1'
            });
            component.preferencesForm.markAsDirty();

            component.onPreferencesSubmit();

            await new Promise(resolve => setTimeout(resolve, 0));
            expect(component.preferencesSuccessMessage()).toBe('Chat preferences updated successfully!');
        });
    });
});

import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Router } from '@angular/router';

import { RegisterComponent } from './register.component';
import { AuthService } from '../../../../core/services/auth.service';

describe('RegisterComponent', () => {
    let authService: { register: ReturnType<typeof vi.fn> };
    let router: { navigate: ReturnType<typeof vi.fn> };

    const fillValid = (component: RegisterComponent) =>
        component.registerForm.setValue({
            username: 'gori',
            email: 'gori@example.com',
            password: 'password1',
            confirmPassword: 'password1',
        });

    beforeEach(async () => {
        authService = { register: vi.fn().mockName('AuthService.register') };
        router = { navigate: vi.fn().mockName('Router.navigate') };

        await TestBed.configureTestingModule({
            imports: [RegisterComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: AuthService, useValue: authService },
                { provide: Router, useValue: router },
            ],
        }).compileComponents();
    });

    it('does not submit while the form is invalid', async () => {
        const component = TestBed.createComponent(RegisterComponent).componentInstance;
        await component.onSubmit();
        expect(authService.register).not.toHaveBeenCalled();
    });

    it('flags mismatched passwords and blocks submit', async () => {
        const component = TestBed.createComponent(RegisterComponent).componentInstance;
        component.registerForm.setValue({
            username: 'gori',
            email: 'gori@example.com',
            password: 'password1',
            confirmPassword: 'password2',
        });

        await component.onSubmit();

        expect(component.registerForm.errors?.['passwordMismatch']).toBe(true);
        expect(authService.register).not.toHaveBeenCalled();
    });

    it('registers with the local provider (no terms_accepted) and navigates', async () => {
        authService.register.mockResolvedValue(undefined);
        const component = TestBed.createComponent(RegisterComponent).componentInstance;
        fillValid(component);

        await component.onSubmit();

        // No terms are collected, so terms_accepted must not be sent.
        expect(authService.register).toHaveBeenCalledWith(
            {
                username: 'gori',
                email: 'gori@example.com',
                password: 'password1',
            },
            'local',
        );
        expect(router.navigate).toHaveBeenCalledWith(['/chat']);
    });

    it('shows an error message when registration fails', async () => {
        authService.register.mockRejectedValue(new Error('An account with this email already exists'));
        const component = TestBed.createComponent(RegisterComponent).componentInstance;
        fillValid(component);

        await component.onSubmit();

        expect(component.errorMessage()).toBe('An account with this email already exists');
        expect(router.navigate).not.toHaveBeenCalled();
        expect(component.isLoading()).toBe(false);
    });

    it('navigates to login', () => {
        const component = TestBed.createComponent(RegisterComponent).componentInstance;
        component.navigateToLogin();
        expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
    });
});

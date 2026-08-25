import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Router, ActivatedRoute } from '@angular/router';

import { LoginComponent } from './login.component';
import { AuthService } from '../../../../core/services/auth.service';

describe('LoginComponent', () => {
    let authService: { login: ReturnType<typeof vi.fn> };
    let router: { navigate: ReturnType<typeof vi.fn> };

    beforeEach(async () => {
        authService = { login: vi.fn().mockName('AuthService.login') };
        router = { navigate: vi.fn().mockName('Router.navigate') };
        const activatedRoute = { snapshot: { queryParams: {} } };

        await TestBed.configureTestingModule({
            imports: [LoginComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: AuthService, useValue: authService },
                { provide: Router, useValue: router },
                { provide: ActivatedRoute, useValue: activatedRoute },
            ],
        }).compileComponents();
    });

    it('does not submit while the form is invalid', async () => {
        const fixture = TestBed.createComponent(LoginComponent);
        const component = fixture.componentInstance;

        await component.onSubmit();

        expect(authService.login).not.toHaveBeenCalled();
    });

    it('logs in with the local provider and navigates to the return url', async () => {
        authService.login.mockResolvedValue(undefined);
        const fixture = TestBed.createComponent(LoginComponent);
        const component = fixture.componentInstance;
        component.loginForm.setValue({ username: 'gori', password: 'password1', remember: false });

        await component.onSubmit();

        expect(authService.login).toHaveBeenCalledWith(
            { username: 'gori', password: 'password1' },
            'local',
        );
        expect(router.navigate).toHaveBeenCalledWith(['/chat']);
    });

    it('shows an error message when login fails', async () => {
        authService.login.mockRejectedValue(new Error('Incorrect username or password'));
        const fixture = TestBed.createComponent(LoginComponent);
        const component = fixture.componentInstance;
        component.loginForm.setValue({ username: 'gori', password: 'password1', remember: false });

        await component.onSubmit();

        expect(component.errorMessage()).toBe('Incorrect username or password');
        expect(router.navigate).not.toHaveBeenCalled();
        expect(component.isLoading()).toBe(false);
    });

    it('navigates to register', () => {
        const fixture = TestBed.createComponent(LoginComponent);
        fixture.componentInstance.navigateToRegister();
        expect(router.navigate).toHaveBeenCalledWith(['/auth/register']);
    });
});

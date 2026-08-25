import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Router } from '@angular/router';

import { AuthService } from '../services/auth.service';
import {
    ExternalAuthProvider,
    NoopExternalAuthProvider,
} from '../auth/external-auth.provider';
import { guestGuard } from './auth.guard';

describe('guestGuard', () => {
    it('redirects authenticated users to chat', async () => {
        const authService = {
            isLoggedIn: vi.fn().mockName("AuthService.isLoggedIn"),
        };
        const router = {
            navigate: vi.fn().mockName("Router.navigate")
        };
        authService.isLoggedIn.mockReturnValue(true);

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                { provide: AuthService, useValue: authService },
                { provide: ExternalAuthProvider, useClass: NoopExternalAuthProvider },
                { provide: Router, useValue: router },
            ],
        });

        const allowed = await TestBed.runInInjectionContext(() => guestGuard({} as any, {} as any));

        expect(allowed).toBe(false);
        expect(router.navigate).toHaveBeenCalledWith(['/chat']);
    });
});

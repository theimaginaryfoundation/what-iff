import type { MockedObject } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, UrlTree } from '@angular/router';
import { firstValueFrom, isObservable, of, throwError } from 'rxjs';

import { personalitySetupGuard } from './personality-setup.guard';
import { PersonalityService } from '../services/personality.service';

describe('personalitySetupGuard', () => {
    let personalityService: Pick<MockedObject<PersonalityService>, 'listPersonalities'>;

    beforeEach(() => {
        personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        } as unknown as Pick<MockedObject<PersonalityService>, 'listPersonalities'>;

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([]),
                { provide: PersonalityService, useValue: personalityService },
            ],
        });
    });

    async function runGuard(url: string): Promise<boolean | UrlTree> {
        const result = TestBed.runInInjectionContext(() => personalitySetupGuard({} as any, { url } as any));
        if (result === true || result === false || result instanceof UrlTree) {
            return result;
        }
        if (isObservable(result)) {
            return firstValueFrom(result) as Promise<boolean | UrlTree>;
        }
        if (result instanceof Promise) {
            return result as Promise<boolean | UrlTree>;
        }
        return result as unknown as boolean | UrlTree;
    }

    it('allows personality routes without checking the API', async () => {
        const value = await runGuard('/personality');
        expect(value).toBe(true);
        expect(personalityService.listPersonalities).not.toHaveBeenCalled();
    });

    it('allows the experimental account restoration route without checking the API', async () => {
        const value = await runGuard('/experimental');
        expect(value).toBe(true);
        expect(personalityService.listPersonalities).not.toHaveBeenCalled();
    });

    it('redirects to personalities when the user has none', async () => {
        personalityService.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        const value = await runGuard('/chat');
        expect(value instanceof UrlTree).toBe(true);
        expect((value as UrlTree).toString()).toBe('/personality?setup=1');
    });

    it('allows app routes when at least one personality exists', async () => {
        personalityService.listPersonalities.mockReturnValue(of({ results: [{ id: 'p-1' } as any], total_count: 1, page: 1 }));

        const value = await runGuard('/chat');
        expect(value).toBe(true);
    });

    it('redirects when the personality API fails', async () => {
        personalityService.listPersonalities.mockReturnValue(throwError(() => new Error('network')));

        const value = await runGuard('/chat');
        expect(value instanceof UrlTree).toBe(true);
        expect((value as UrlTree).toString()).toBe('/personality?setup=1');
    });
});

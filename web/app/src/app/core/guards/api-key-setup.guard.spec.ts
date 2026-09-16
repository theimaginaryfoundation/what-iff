import type { MockedObject } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, UrlTree } from '@angular/router';
import { firstValueFrom, isObservable, of, throwError } from 'rxjs';

import { apiKeySetupGuard } from './api-key-setup.guard';
import { ProviderKeyService } from '../services/provider-key.service';
import { ProviderKeyStatus } from '../models/provider-key.model';

function status(overrides: Partial<ProviderKeyStatus> = {}): ProviderKeyStatus {
    return {
        provider: 'openai',
        configured: false,
        source: '',
        required: true,
        supported: true,
        ...overrides,
    };
}

describe('apiKeySetupGuard', () => {
    let providerKeys: Pick<MockedObject<ProviderKeyService>, 'listStatuses'>;

    beforeEach(() => {
        providerKeys = {
            listStatuses: vi.fn().mockName("ProviderKeyService.listStatuses")
        } as unknown as Pick<MockedObject<ProviderKeyService>, 'listStatuses'>;

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([]),
                { provide: ProviderKeyService, useValue: providerKeys },
            ],
        });
    });

    async function runGuard(url: string): Promise<boolean | UrlTree> {
        const result = TestBed.runInInjectionContext(() => apiKeySetupGuard({} as any, { url } as any));
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

    it('lets the integrations screen through without checking, so the user is not trapped', async () => {
        expect(await runGuard('/integrations')).toBe(true);
        expect(providerKeys.listStatuses).not.toHaveBeenCalled();
    });

    // The key screen links here to explain what a key is spent on. Gating it
    // makes that link bounce back to the screen it was offered from, while
    // someone is deciding whether to paste a credential in.
    it('lets the providers explainer through so the key screen\'s own link works', async () => {
        expect(await runGuard('/providers')).toBe(true);
    });

    it('lets the profile screen through so sign-out stays reachable', async () => {
        expect(await runGuard('/profile')).toBe(true);
        expect(providerKeys.listStatuses).not.toHaveBeenCalled();
    });

    it('redirects to the API key setup when a required provider is unconfigured', async () => {
        providerKeys.listStatuses.mockReturnValue(of([status({ configured: false })]));
        const value = await runGuard('/chat');
        expect(value).toBeInstanceOf(UrlTree);
        expect((value as UrlTree).toString()).toContain('/integrations');
        expect((value as UrlTree).toString()).toContain('setup=api-key');
    });

    it('allows through once the required provider is configured', async () => {
        providerKeys.listStatuses.mockReturnValue(of([status({ configured: true, source: 'account' })]));
        expect(await runGuard('/chat')).toBe(true);
    });

    it('counts the deployment fallback as configured', async () => {
        providerKeys.listStatuses.mockReturnValue(of([status({ configured: true, source: 'deployment' })]));
        expect(await runGuard('/chat')).toBe(true);
    });

    it('ignores providers that are not required', async () => {
        providerKeys.listStatuses.mockReturnValue(of([
            status({ configured: true, source: 'account' }),
            status({ provider: 'anthropic', configured: false, required: false, supported: false }),
        ]));
        expect(await runGuard('/chat')).toBe(true);
    });

    // A transient failure must not strand someone on a setup screen; the next
    // request they make will surface the real problem.
    it('allows through when the status lookup fails', async () => {
        providerKeys.listStatuses.mockReturnValue(throwError(() => new Error('network down')));
        expect(await runGuard('/chat')).toBe(true);
    });
});

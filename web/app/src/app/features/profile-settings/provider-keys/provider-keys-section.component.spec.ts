import { HttpErrorResponse } from '@angular/common/http';
import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { of, throwError } from 'rxjs';

import { ProviderKeySummary } from '../../../core/models/provider-key.model';
import { ConfirmationService } from '../../../core/services/confirmation.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { ProviderKeysService } from '../../../core/services/provider-keys.service';
import { ProfileSettingsModalService } from '../profile-settings-modal.service';
import { ProviderKeysSectionComponent } from './provider-keys-section.component';

const OPENAI: ProviderKeySummary = { provider: 'openai', hint: 'abcd', updated_at: '2026-10-15T12:00:00Z' };

describe('ProviderKeysSectionComponent', () => {
  let keys: {
    list: ReturnType<typeof vi.fn>;
    set: ReturnType<typeof vi.fn>;
    remove: ReturnType<typeof vi.fn>;
    refreshAccess: ReturnType<typeof vi.fn>;
  };
  let personalities: { listPersonalities: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };
  let router: { navigate: ReturnType<typeof vi.fn> };
  let modal: ProfileSettingsModalService;

  async function render(): Promise<ComponentFixture<ProviderKeysSectionComponent>> {
    const fixture = TestBed.createComponent(ProviderKeysSectionComponent);
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.loading()).toBe(false));
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture;
  }

  function row(fixture: ComponentFixture<ProviderKeysSectionComponent>, provider: string): HTMLElement {
    return fixture.nativeElement.querySelector(`[data-provider="${provider}"]`);
  }

  beforeEach(async () => {
    keys = {
      list: vi.fn().mockReturnValue(of([])),
      set: vi.fn().mockReturnValue(of(OPENAI)),
      remove: vi.fn().mockReturnValue(of(undefined)),
      refreshAccess: vi.fn().mockReturnValue(of(null)),
    };
    personalities = { listPersonalities: vi.fn().mockReturnValue(of({ results: [{ id: 'p1' }], total_count: 1, page: 1 })) };
    confirmation = { confirm: vi.fn().mockResolvedValue(true) };
    router = { navigate: vi.fn() };

    await TestBed.configureTestingModule({
      imports: [ProviderKeysSectionComponent],
      providers: [
        provideZonelessChangeDetection(),
        ProfileSettingsModalService,
        { provide: ProviderKeysService, useValue: keys },
        { provide: PersonalityService, useValue: personalities },
        { provide: ConfirmationService, useValue: confirmation },
        { provide: Router, useValue: router },
      ],
    }).compileComponents();
    modal = TestBed.inject(ProfileSettingsModalService);
  });

  it('lists every provider, OpenAI first and marked required', async () => {
    const fixture = await render();
    const names = [...fixture.nativeElement.querySelectorAll('.provider-keys__name strong')].map((el: Element) => el.textContent?.trim());

    expect(names).toEqual(['OpenAI', 'Anthropic', 'Google Gemini', 'z.ai', 'Mistral', 'DeepSeek', 'Qwen', 'Xiaomi MiMo']);
    expect(row(fixture, 'openai').querySelector('.provider-keys__badge')?.textContent?.trim()).toBe('Required');
    expect(row(fixture, 'anthropic').querySelector('.provider-keys__badge')).toBeNull();
  });

  it('shows a stored key by its last four characters only', async () => {
    keys.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();

    expect(row(fixture, 'openai').querySelector('.provider-keys__hint')?.textContent).toContain('••••abcd');
    expect(row(fixture, 'openai').textContent).toContain('Replace');
    expect(row(fixture, 'anthropic').textContent).toContain('Not set');
  });

  it('saves a key with the password, then reloads keys and access', async () => {
    const fixture = await render();
    const component = fixture.componentInstance;
    component.open('openai');
    component.keyInput.set('  sk-test-abcd  ');
    component.passwordInput.set('hunter2');

    await component.save({ id: 'openai', name: 'OpenAI', usedFor: '' });

    expect(keys.set).toHaveBeenCalledWith('openai', 'sk-test-abcd', 'hunter2');
    expect(keys.refreshAccess).toHaveBeenCalled();
    expect(keys.list).toHaveBeenCalledTimes(2);
    expect(component.editing()).toBeNull();
    expect(component.notice()).toBe('OpenAI key saved.');
    expect(component.passwordInput()).toBe('');
  });

  it('asks for both fields before sending anything', async () => {
    const fixture = await render();
    const component = fixture.componentInstance;
    component.open('openai');

    await component.save({ id: 'openai', name: 'OpenAI', usedFor: '' });
    expect(component.formError()).toBe('Paste your OpenAI key.');

    component.keyInput.set('sk-test-abcd');
    await component.save({ id: 'openai', name: 'OpenAI', usedFor: '' });
    expect(component.formError()).toBe('Enter your password to confirm.');
    expect(keys.set).not.toHaveBeenCalled();
  });

  it('says so when the password is wrong, and keeps the form open', async () => {
    keys.set.mockReturnValue(
      throwError(() => new HttpErrorResponse({ status: 403, error: { code: 'reauthentication_required', message: 'Confirm your current password' } })),
    );
    const fixture = await render();
    const component = fixture.componentInstance;
    component.open('openai');
    component.keyInput.set('sk-test-abcd');
    component.passwordInput.set('wrong');

    await component.save({ id: 'openai', name: 'OpenAI', usedFor: '' });

    expect(component.formError()).toBe("That password didn't match. Try again.");
    expect(component.editing()).toBe('openai');
    expect(component.keyInput()).toBe('sk-test-abcd');
    expect(component.passwordInput()).toBe('');
  });

  it("shows the server's reason for a rejected key", async () => {
    keys.set.mockReturnValue(
      throwError(() => new HttpErrorResponse({ status: 400, error: { code: 'bad_request', message: 'provider key must be 8 to 512 characters, without spaces' } })),
    );
    const fixture = await render();
    const component = fixture.componentInstance;
    component.open('openai');
    component.keyInput.set('short');
    component.passwordInput.set('hunter2');

    await component.save({ id: 'openai', name: 'OpenAI', usedFor: '' });

    expect(component.formError()).toBe('provider key must be 8 to 512 characters, without spaces');
  });

  it('removes a key after confirming, without a password', async () => {
    keys.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();

    await fixture.componentInstance.remove({ id: 'openai', name: 'OpenAI', usedFor: '', required: true });

    expect(confirmation.confirm).toHaveBeenCalledWith(expect.objectContaining({ type: 'danger', confirmText: 'Remove key' }));
    expect(keys.remove).toHaveBeenCalledWith('openai');
    expect(keys.refreshAccess).toHaveBeenCalled();
  });

  it('keeps the key when removal is cancelled', async () => {
    confirmation.confirm.mockResolvedValue(false);
    keys.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();

    await fixture.componentInstance.remove({ id: 'openai', name: 'OpenAI', usedFor: '' });

    expect(keys.remove).not.toHaveBeenCalled();
  });

  describe('during onboarding', () => {
    beforeEach(() => {
      personalities.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
    });

    it('points on to creating a personality once the OpenAI key is in', async () => {
      keys.list.mockReturnValue(of([OPENAI]));
      const fixture = await render();
      const next = fixture.nativeElement.querySelector('[data-testid="provider-keys-next"]') as HTMLElement;
      expect(next).not.toBeNull();

      modal.open('api-keys');
      next.querySelector('button')!.click();

      expect(modal.openState()).toBe(false);
      expect(router.navigate).toHaveBeenCalledWith(['/personality']);
    });

    it('does not point on before the required key is added', async () => {
      const fixture = await render();
      expect(fixture.nativeElement.querySelector('[data-testid="provider-keys-next"]')).toBeNull();
    });
  });

  it('does not point on for an account that already has a personality', async () => {
    keys.list.mockReturnValue(of([OPENAI]));
    const fixture = await render();
    expect(fixture.nativeElement.querySelector('[data-testid="provider-keys-next"]')).toBeNull();
  });
});

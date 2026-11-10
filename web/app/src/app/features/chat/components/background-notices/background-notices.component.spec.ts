import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, Subject, throwError } from 'rxjs';

import { BackgroundNotice } from '../../../../core/models/utility-model.model';
import { BackgroundNoticesService } from '../../../../core/services/background-notices.service';
import { ProviderKeysService } from '../../../../core/services/provider-keys.service';
import { ProfileSettingsModalService } from '../../../profile-settings/profile-settings-modal.service';
import { BackgroundNoticesComponent } from './background-notices.component';

function notice(overrides: Partial<BackgroundNotice>): BackgroundNotice {
  return {
    id: 'n1',
    chat_id: 'chat-1',
    step: 'memory',
    code: 'provider_error',
    count: 1,
    created_at: '2026-11-05T12:00:00Z',
    updated_at: '2026-11-05T12:00:00Z',
    ...overrides,
  };
}

@Component({
  standalone: true,
  imports: [BackgroundNoticesComponent],
  template: `<app-background-notices [chatId]="chatId()" [settled]="settled()" />`,
})
class HostComponent {
  chatId = signal<string | null>('chat-1');
  settled = signal(0);
}

describe('BackgroundNoticesComponent', () => {
  let service: { list: ReturnType<typeof vi.fn>; dismiss: ReturnType<typeof vi.fn> };
  let settings: { open: ReturnType<typeof vi.fn> };
  let accountsSupplyKeys: ReturnType<typeof signal<boolean>>;
  let fixture: ComponentFixture<HostComponent>;

  async function render(notices: BackgroundNotice[]): Promise<HTMLElement> {
    service.list.mockReturnValue(of(notices));
    fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    return fixture.nativeElement as HTMLElement;
  }

  function texts(el: HTMLElement): string[] {
    return Array.from(el.querySelectorAll('[data-testid="background-notice"]')).map(n => n.textContent?.replace(/\s+/g, ' ').trim() ?? '');
  }

  beforeEach(async () => {
    service = { list: vi.fn(), dismiss: vi.fn(() => of(undefined)) };
    settings = { open: vi.fn() };
    accountsSupplyKeys = signal(true);
    await TestBed.configureTestingModule({
      imports: [HostComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: BackgroundNoticesService, useValue: service },
        { provide: ProfileSettingsModalService, useValue: settings },
        { provide: ProviderKeysService, useValue: { accountsSupplyKeys } },
      ],
    }).compileComponents();
  });

  it('shows a missing key with the step, the provider and the way to add one', async () => {
    const el = await render([notice({ code: 'provider_not_configured', provider: 'Anthropic', model: 'claude-haiku-4-5', count: 3 })]);
    expect(service.list).toHaveBeenCalledWith('chat-1');
    expect(texts(el)[0]).toContain("Memories from this chat weren't saved.");
    expect(texts(el)[0]).toContain("There's no Anthropic key for claude-haiku-4-5.");
    expect(texts(el)[0]).toContain('This has happened 3 times.');
    const fix = el.querySelector('[data-testid="background-notice-fix"]') as HTMLButtonElement;
    expect(fix.textContent?.trim()).toBe('Add an Anthropic key');
    fix.click();
    expect(settings.open).toHaveBeenCalledWith('providers');
  });

  it('offers no fix where the deployment supplies the keys, since it chooses the models', async () => {
    accountsSupplyKeys.set(false);
    const el = await render([notice({ code: 'provider_not_configured', provider: 'Mistral' }), notice({ id: 'b', step: 'fallback_memory', code: 'model_unavailable' })]);
    expect(texts(el).length).toBe(2);
    expect(el.querySelector('[data-testid="background-notice-fix"]')).toBeNull();
    expect(texts(el)[0]).toContain("Mistral isn't set up on this server.");
    expect(texts(el)[0]).not.toContain('Add one');
  });

  it('offers to change background models for a provider error', async () => {
    const el = await render([notice({ provider: 'Mistral' })]);
    const fix = el.querySelector('[data-testid="background-notice-fix"]') as HTMLButtonElement;
    expect(fix.textContent?.trim()).toBe('Change background models');
    fix.click();
    expect(settings.open).toHaveBeenCalledWith('profile');
  });

  it('says a provider error is retried, and names a fallback by its role', async () => {
    const el = await render([
      notice({ id: 'a', step: 'summary', provider: 'OpenAI', model: 'gpt-5.6-luna' }),
      notice({ id: 'b', step: 'fallback_memory', code: 'model_unavailable' }),
    ]);
    const [error, fallback] = texts(el);
    expect(error).toContain("This chat's summary wasn't updated.");
    expect(error).toContain('OpenAI returned an error (gpt-5.6-luna). It runs again as the chat goes on, and this goes away once it works.');
    expect(fallback).toContain("The model chosen for Memory and summaries wasn't available.");
  });

  it("names a role this app doesn't know in general terms, not by its raw name", async () => {
    const el = await render([notice({ step: 'fallback_portraits', code: 'model_unavailable' })]);
    expect(texts(el)[0]).toContain("A model chosen for background work wasn't available.");
    expect(texts(el)[0]).not.toContain('portraits');
  });

  it("never shows one chat's notices on another, even if the new chat's load fails", async () => {
    const el = await render([notice({})]);
    expect(texts(el).length).toBe(1);
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    service.list.mockReturnValue(throwError(() => new Error('offline')));
    fixture.componentInstance.chatId.set('chat-2');
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(texts(el)).toEqual([]);
    expect(warn).toHaveBeenCalled();
    warn.mockRestore();
  });

  it('words a notice once it knows who supplies keys, even when that loads after the notices', async () => {
    accountsSupplyKeys.set(false);
    const el = await render([notice({ code: 'provider_not_configured', provider: 'Anthropic', model: 'claude-haiku-4-5' })]);
    expect(texts(el)[0]).toContain("Anthropic isn't set up on this server");

    accountsSupplyKeys.set(true);
    fixture.detectChanges();
    expect(texts(el)[0]).toContain("There's no Anthropic key for claude-haiku-4-5.");
    expect(el.querySelector('[data-testid="background-notice-fix"]')).not.toBeNull();
  });

  it("doesn't bring back a dismissed notice from a list that was already on its way", async () => {
    const el = await render([notice({})]);
    const inFlight = new Subject<BackgroundNotice[]>();
    service.list.mockReturnValue(inFlight);
    fixture.componentInstance.settled.set(1);
    fixture.detectChanges();

    (el.querySelector('[data-testid="background-notice-dismiss"]') as HTMLButtonElement).click();
    inFlight.next([notice({})]);
    inFlight.complete();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(texts(el)).toEqual([]);
  });

  it("puts a notice back if dismissing fails, but never on a chat the person has moved to", async () => {
    const el = await render([notice({})]);
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const failing = new Subject<void>();
    service.dismiss.mockReturnValue(failing);
    (el.querySelector('[data-testid="background-notice-dismiss"]') as HTMLButtonElement).click();

    service.list.mockReturnValue(of([]));
    fixture.componentInstance.chatId.set('chat-2');
    fixture.detectChanges();
    await fixture.whenStable();
    service.list.mockClear();
    service.list.mockReturnValue(of([notice({})]));
    failing.error(new Error('offline'));
    await fixture.whenStable();
    fixture.detectChanges();
    expect(service.list).not.toHaveBeenCalled();
    expect(texts(el)).toEqual([]);
    expect(warn).toHaveBeenCalledWith('Failed to dismiss a background notice:', expect.any(Error));
    warn.mockRestore();
  });

  it('dismisses a notice, and reads them again when a turn settles', async () => {
    const el = await render([notice({})]);
    (el.querySelector('[data-testid="background-notice-dismiss"]') as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(service.dismiss).toHaveBeenCalledWith('chat-1', 'n1');
    expect(texts(el)).toEqual([]);

    service.list.mockReturnValue(of([notice({ id: 'n2', step: 'scratchpad' })]));
    fixture.componentInstance.settled.set(1);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(texts(el)[0]).toContain("The scratchpad wasn't updated from this chat.");
  });

  it('says no model is chosen, with the way to choose one, and only informs where the server chooses', async () => {
    const el = await render([notice({ code: 'model_not_set', role: 'memory' })]);
    expect(texts(el)[0]).toContain("Memories from this chat weren't saved.");
    expect(texts(el)[0]).toContain('No model is chosen for Memory and summaries.');
    const fix = el.querySelector('[data-testid="background-notice-fix"]') as HTMLButtonElement;
    expect(fix.textContent?.trim()).toBe('Choose background models');
    fix.click();
    expect(settings.open).toHaveBeenCalledWith('profile');

    accountsSupplyKeys.set(false);
    fixture.detectChanges();
    expect(texts(el)[0]).toContain("This server hasn't chosen a model for it.");
    expect(el.querySelector('[data-testid="background-notice-fix"]')).toBeNull();
  });

  it("shows a role's notices with one cause as one, and dismisses them together", async () => {
    const cause = { code: 'provider_error' as const, role: 'simple' as const, provider: 'Mistral', model: 'mistral-small-latest' };
    const el = await render([
      notice({ id: 'a', step: 'chat_name', ...cause }),
      notice({ id: 'b', step: 'expression_pick', ...cause, count: 2 }),
      notice({ id: 'c', step: 'memory_query', role: 'simple', code: 'model_not_set' }),
    ]);
    expect(texts(el).length).toBe(2);
    expect(texts(el)[0]).toContain("Simple tasks didn't run on this chat: naming the chat and picking expressions.");
    expect(texts(el)[0]).toContain('Mistral returned an error (mistral-small-latest).');
    expect(texts(el)[0]).toContain('This has happened 2 times.');
    expect(texts(el)[1]).toContain("Replies in this chat aren't drawing on memories.");

    (el.querySelector('[data-testid="background-notice-dismiss"]') as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(service.dismiss).toHaveBeenCalledWith('chat-1', 'a');
    expect(service.dismiss).toHaveBeenCalledWith('chat-1', 'b');
    expect(texts(el).length).toBe(1);
  });

  it("says when a role's chosen model can't be used, not that a provider failed", async () => {
    const el = await render([notice({ step: 'chat_name', role: 'simple', code: 'model_unavailable' })]);
    expect(texts(el)[0]).toContain("This chat wasn't named.");
    expect(texts(el)[0]).toContain("The model chosen for Simple tasks can't be used any more");
    expect(texts(el)[0]).not.toContain('returned an error');
  });
});

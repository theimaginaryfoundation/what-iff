import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { HttpErrorResponse } from '@angular/common/http';
import { of, throwError } from 'rxjs';

import { ConfirmationService } from '../../core/services/confirmation.service';
import { RemoteSyncService, RemoteSyncSettings } from './remote-sync.service';
import { RemoteSyncTabComponent } from './remote-sync-tab.component';

const NOT_SET_UP: RemoteSyncSettings = {
  configured: false,
  running: false,
  interval_seconds: 300,
  default_base_url: 'https://api.whatiff.chat',
};

const IN_SYNC: RemoteSyncSettings = {
  configured: true,
  enabled: true,
  base_url: 'https://api.whatiff.chat',
  token_hint: 'wxyz',
  status: 'ok',
  running: false,
  last_success_at: '2026-10-13T20:00:00Z',
  last_run: {
    threads_checked: 3,
    threads_created: 2,
    threads_adopted: 0,
    threads_updated: 1,
    threads_forked: 0,
    threads_failed: 0,
    threads_deferred: 0,
    messages_added: 40,
  },
  totals: { threads: 12, messages: 1204, forks: 1 },
  interval_seconds: 300,
  default_base_url: 'https://api.whatiff.chat',
};

describe('RemoteSyncTabComponent', () => {
  let fixture: ComponentFixture<RemoteSyncTabComponent>;
  let component: RemoteSyncTabComponent;
  let api: Pick<MockedObject<RemoteSyncService>, 'get' | 'save' | 'remove' | 'runNow'>;
  let confirmation: Pick<MockedObject<ConfirmationService>, 'confirm'>;

  const el = (id: string) => fixture.nativeElement.querySelector(`[data-testid="${id}"]`) as HTMLElement | null;
  const text = (id: string) => el(id)?.textContent?.replace(/\s+/g, ' ').trim() ?? '';

  async function render(settings: RemoteSyncSettings) {
    api.get.mockReturnValue(of(settings));
    fixture = TestBed.createComponent(RemoteSyncTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function type(id: string, value: string) {
    const input = el(id) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  }

  async function click(id: string) {
    (el(id) as HTMLButtonElement).click();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  beforeEach(async () => {
    api = {
      get: vi.fn().mockName('RemoteSyncService.get'),
      save: vi.fn().mockName('RemoteSyncService.save'),
      remove: vi.fn().mockName('RemoteSyncService.remove'),
      runNow: vi.fn().mockName('RemoteSyncService.runNow'),
    };
    confirmation = { confirm: vi.fn().mockName('ConfirmationService.confirm') };
    await TestBed.configureTestingModule({
      imports: [RemoteSyncTabComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: RemoteSyncService, useValue: api },
        { provide: ConfirmationService, useValue: confirmation },
      ],
    }).compileComponents();
  });

  afterEach(() => fixture?.destroy());

  it('walks through setup when nothing is set up, with the hosted address filled in', async () => {
    await render(NOT_SET_UP);

    expect(text('remote-sync-status')).toContain('Read threads');
    expect((el('remote-sync-url') as HTMLInputElement).value).toBe('https://api.whatiff.chat');
    expect((el('remote-sync-save') as HTMLButtonElement).disabled).toBe(true);
    expect(el('remote-sync-remove')).toBeNull();
  });

  it('saves a pasted token and clears it from the form', async () => {
    await render(NOT_SET_UP);
    api.save.mockReturnValue(of({ ...IN_SYNC, status: 'pending', last_run: undefined, last_success_at: null }));

    type('remote-sync-token', '  wht_secret  ');
    expect((el('remote-sync-save') as HTMLButtonElement).disabled).toBe(false);
    await click('remote-sync-save');

    expect(api.save).toHaveBeenCalledWith({ base_url: 'https://api.whatiff.chat', enabled: true, token: 'wht_secret' });
    expect(component.token()).toBe('');
    expect(text('remote-sync-status-badge')).toBe('Waiting for the first sync');
  });

  it('shows why the remote refused the token', async () => {
    await render(NOT_SET_UP);
    api.save.mockReturnValue(
      throwError(
        () =>
          new HttpErrorResponse({
            status: 400,
            error: { error: 'The token cannot read threads. Create one with “Read threads” (chat:read).' },
          }),
      ),
    );
    type('remote-sync-token', 'wht_x');
    await click('remote-sync-save');

    expect(text('remote-sync-error')).toContain('cannot read threads');
  });

  it('shows status, totals and the last run once set up, and saves without a new token', async () => {
    await render(IN_SYNC);

    expect(text('remote-sync-status-badge')).toBe('In sync');
    expect(text('remote-sync-threads')).toBe('12');
    expect(text('remote-sync-last-run')).toContain('2 new threads, 1 updated thread, 40 messages');
    expect((el('remote-sync-token') as HTMLInputElement).placeholder).toContain('wxyz');

    api.save.mockReturnValue(of(IN_SYNC));
    await click('remote-sync-save');
    expect(api.save).toHaveBeenCalledWith({ base_url: 'https://api.whatiff.chat', enabled: true });
  });

  it('flags a refused token as needing attention', async () => {
    await render({ ...IN_SYNC, status: 'needs_attention', last_error: 'The remote server did not accept the token.' });
    expect(text('remote-sync-status-badge')).toBe('Needs attention');
    expect(text('remote-sync-last-error')).toContain('did not accept');
  });

  it('runs now and follows the run until it ends', async () => {
    await render(IN_SYNC);
    component.pollMs = 5;
    api.runNow.mockReturnValue(of({ ...IN_SYNC, running: true }));
    api.get.mockReturnValue(of({ ...IN_SYNC, running: true }));

    await click('remote-sync-run-now');
    expect(text('remote-sync-status-badge')).toBe('Syncing…');
    expect((el('remote-sync-run-now') as HTMLButtonElement).disabled).toBe(true);

    api.get.mockReturnValue(of({ ...IN_SYNC, totals: { threads: 13, messages: 1210, forks: 1 } }));
    await vi.waitFor(() => {
      fixture.detectChanges();
      expect(text('remote-sync-status-badge')).toBe('In sync');
    });
    expect(text('remote-sync-threads')).toBe('13');
  });

  it('stops syncing only after confirming', async () => {
    await render(IN_SYNC);
    confirmation.confirm.mockResolvedValue(false);
    await click('remote-sync-remove');
    expect(api.remove).not.toHaveBeenCalled();

    confirmation.confirm.mockResolvedValue(true);
    api.remove.mockReturnValue(of(undefined));
    await click('remote-sync-remove');
    expect(api.remove).toHaveBeenCalled();
    expect(el('remote-sync-remove')).toBeNull();
    expect(text('remote-sync-status')).toContain('Read threads');
  });
});

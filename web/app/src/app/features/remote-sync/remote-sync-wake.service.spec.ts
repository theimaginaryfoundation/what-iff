import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { Subject, of, throwError } from 'rxjs';

import { AuthService } from '../../core/services/auth.service';
import { PersonalityChange, PersonalityService } from '../../core/services/personality.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { RemoteSyncService, RemoteSyncSettings } from './remote-sync.service';
import { RemoteSyncWakeService } from './remote-sync-wake.service';

describe('RemoteSyncWakeService', () => {
  const loggedIn = signal(false);
  let api: {
    available: ReturnType<typeof vi.fn>;
    wake: ReturnType<typeof vi.fn>;
    get: ReturnType<typeof vi.fn>;
  };
  let threadList: { refresh: ReturnType<typeof vi.fn> };
  let service: RemoteSyncWakeService;
  let visibility: DocumentVisibilityState;
  let personaChanges: Subject<PersonalityChange>;

  const done = (messages: number): RemoteSyncSettings => ({
    configured: true,
    running: false,
    last_run: {
      threads_checked: 1,
      threads_created: 0,
      threads_adopted: 0,
      threads_updated: messages ? 1 : 0,
      threads_forked: 0,
      threads_failed: 0,
      threads_deferred: 0,
      messages_added: messages,
    },
  });

  beforeEach(() => {
    loggedIn.set(false);
    visibility = 'visible';
    api = { available: vi.fn(() => of(true)), wake: vi.fn(() => of({ started: false, rematched: 0 })), get: vi.fn() };
    personaChanges = new Subject<PersonalityChange>();
    threadList = { refresh: vi.fn(() => Promise.resolve()) };
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: RemoteSyncService, useValue: api },
        { provide: AuthService, useValue: { isLoggedIn: loggedIn } },
        { provide: ThreadListService, useValue: threadList },
        { provide: PersonalityService, useValue: { changes$: personaChanges.asObservable() } },
      ],
    });
    service = TestBed.inject(RemoteSyncWakeService);
    service.pollMs = 1;
  });

  it('wakes the server when a signed-in session starts, not before', async () => {
    service.start();
    TestBed.tick();
    expect(api.wake).not.toHaveBeenCalled();

    loggedIn.set(true);
    TestBed.tick();
    await vi.waitFor(() => expect(api.wake).toHaveBeenCalledTimes(1));
  });

  it('follows a run it started and refreshes the thread list when something came over', async () => {
    api.wake.mockReturnValue(of({ started: true, rematched: 0 }));
    api.get.mockReturnValueOnce(of({ configured: true, running: true })).mockReturnValueOnce(of(done(4)));
    await service.wake();
    expect(api.get).toHaveBeenCalledTimes(2);
    expect(threadList.refresh).toHaveBeenCalledTimes(1);
  });

  it('leaves the thread list alone when the run brought nothing', async () => {
    api.wake.mockReturnValue(of({ started: true, rematched: 0 }));
    api.get.mockReturnValue(of(done(0)));
    await service.wake();
    expect(threadList.refresh).not.toHaveBeenCalled();
  });

  it('sends nothing on a server without mullet mode, and swallows failures', async () => {
    api.available.mockReturnValue(of(false));
    await service.wake();
    expect(api.wake).not.toHaveBeenCalled();

    api.available.mockReturnValue(of(true));
    api.wake.mockReturnValue(throwError(() => new Error('offline')));
    await expect(service.wake()).resolves.toBeUndefined();
  });

  it('wakes again only after the tab was away for a while', async () => {
    loggedIn.set(true);
    service.start();
    TestBed.tick();
    await vi.waitFor(() => expect(api.wake).toHaveBeenCalledTimes(1));
    const doc = TestBed.inject(DOCUMENT);
    const now = vi.spyOn(Date, 'now');

    now.mockReturnValue(1_000_000);
    visibility = 'hidden';
    doc.dispatchEvent(new Event('visibilitychange'));
    now.mockReturnValue(1_000_000 + 60_000);
    visibility = 'visible';
    doc.dispatchEvent(new Event('visibilitychange'));
    await Promise.resolve();
    expect(api.wake).toHaveBeenCalledTimes(1);

    now.mockReturnValue(2_000_000);
    visibility = 'hidden';
    doc.dispatchEvent(new Event('visibilitychange'));
    now.mockReturnValue(2_000_000 + RemoteSyncWakeService.IDLE_MS);
    visibility = 'visible';
    doc.dispatchEvent(new Event('visibilitychange'));
    await vi.waitFor(() => expect(api.wake).toHaveBeenCalledTimes(2));
    now.mockRestore();
  });
});

describe('RemoteSyncWakeService and personas', () => {
  it('wakes when a persona is created or imported, and refreshes when copies took it', async () => {
    const personaChanges = new Subject<PersonalityChange>();
    const api = { available: vi.fn(() => of(true)), wake: vi.fn(() => of({ started: false, rematched: 2 })), get: vi.fn() };
    const threadList = { refresh: vi.fn(() => Promise.resolve()) };
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        { provide: RemoteSyncService, useValue: api },
        { provide: AuthService, useValue: { isLoggedIn: signal(false) } },
        { provide: ThreadListService, useValue: threadList },
        { provide: PersonalityService, useValue: { changes$: personaChanges.asObservable() } },
      ],
    });
    const service = TestBed.inject(RemoteSyncWakeService);
    service.start();

    personaChanges.next({ kind: 'updated', personality: {} as never });
    await Promise.resolve();
    expect(api.wake).not.toHaveBeenCalled();

    personaChanges.next({ kind: 'created', personality: {} as never });
    await vi.waitFor(() => expect(threadList.refresh).toHaveBeenCalledTimes(1));
    expect(api.wake).toHaveBeenCalledTimes(1);
  });
});

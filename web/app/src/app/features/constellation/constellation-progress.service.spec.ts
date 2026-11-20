import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';
import { AuthService } from '../../core/services/auth.service';
import { ConstellationProgressService } from './constellation-progress.service';

describe('ConstellationProgressService', () => {
  const currentUser = signal<{ id: string } | null>({ id: 'learner-a' });
  let service: ConstellationProgressService;
  beforeEach(() => {
    localStorage.clear();
    currentUser.set({ id: 'learner-a' });
    TestBed.configureTestingModule({ providers: [{ provide: AuthService, useValue: { currentUser } }] });
    service = TestBed.inject(ConstellationProgressService);
    TestBed.tick();
  });
  afterEach(() => {
    vi.restoreAllMocks();
    localStorage.clear();
  });

  it('requires a note and satisfied prerequisites and awards each objective once', () => {
    expect(service.complete('S2', 'I checked the revised result.')).toBe(false);
    expect(service.complete('S1', 'done')).toBe(false);
    expect(service.complete('S1', 'I created and tested my personality.')).toBe(true);
    expect(service.complete('S1', 'I created and tested my personality.')).toBe(false);
    expect(service.xp()).toBe(50);
  });

  it('isolates browser progress by account and clears visible credit immediately on switch/logout', () => {
    service.complete('S1', 'I created and tested my personality.');
    currentUser.set({ id: 'learner-b' });
    expect(service.completed().size).toBe(0);
    expect(service.complete('S2', 'I checked the revised result.')).toBe(false);
    TestBed.tick();
    expect(service.progress()).toEqual({});
    currentUser.set({ id: 'learner-a' });
    TestBed.tick();
    expect(service.completed().has('S1')).toBe(true);
    currentUser.set(null);
    expect(service.completed().size).toBe(0);
    TestBed.tick();
    expect(service.progress()).toEqual({});
  });

  it('retains session progress and reports blocked storage without failing the app', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('Quota');
    });
    expect(service.complete('S1', 'I created and tested my personality.')).toBe(true);
    expect(service.completed().has('S1')).toBe(true);
    expect(service.persisted()).toBe(false);
  });
});

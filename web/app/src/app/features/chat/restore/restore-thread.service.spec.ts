import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';
import { Chat } from '../../../core/models/chat.model';
import { ThreadListService } from '../../../core/services/thread-list.service';
import { RestoreThreadService } from './restore-thread.service';

const chat = (id: string, archived = true): Chat => ({ id, archived, name: id, user_id: 'owner', created_at: '', updated_at: '' });

describe('RestoreThreadService', () => {
  let service: RestoreThreadService;
  let threads: { setThreadArchived: ReturnType<typeof vi.fn>; setAllSelected: ReturnType<typeof vi.fn>; error: ReturnType<typeof signal<string | null>> };
  beforeEach(() => {
    threads = { setThreadArchived: vi.fn().mockResolvedValue(true), setAllSelected: vi.fn(), error: signal<string | null>(null) };
    TestBed.configureTestingModule({ providers: [{ provide: ThreadListService, useValue: threads }] });
    service = TestBed.inject(RestoreThreadService);
  });

  it('opening and cancelling never mutates or prepares a thread', () => {
    service.request([chat('a')]);
    service.guidance.set('Keep the decisions');
    expect(threads.setThreadArchived).not.toHaveBeenCalled();
    service.cancel();
    expect(service.targets()).toEqual([]);
    expect(service.guidance()).toBe('');
    expect(threads.setThreadArchived).not.toHaveBeenCalled();
  });

  it('submits trimmed guidance once after confirmation and clears successful selection', async () => {
    const target = chat('a');
    service.request([target, target, chat('active', false)]);
    service.guidance.set('  Keep decisions; omit drafts.  ');
    const pending = service.confirm();
    await service.confirm();
    await pending;
    expect(threads.setThreadArchived).toHaveBeenCalledTimes(1);
    expect(threads.setThreadArchived).toHaveBeenCalledWith(target, false, 'Keep decisions; omit drafts.');
    expect(threads.setAllSelected).toHaveBeenCalledWith(['a'], false);
    expect(service.targets()).toEqual([]);
  });

  it('keeps instructions and only failed bulk targets for retry', async () => {
    threads.setThreadArchived.mockResolvedValueOnce(true).mockResolvedValueOnce(false);
    service.request([chat('a'), chat('b')]);
    service.guidance.set('Keep final decisions');
    await service.confirm();
    expect(service.targets().map(chat => chat.id)).toEqual(['b']);
    expect(service.guidance()).toBe('Keep final decisions');
    expect(service.error()).toContain('Could not unarchive');
    await service.confirm();
    expect(threads.setThreadArchived).toHaveBeenLastCalledWith(chat('b'), false, 'Keep final decisions');
    expect(service.targets()).toEqual([]);
  });

  it('empty guidance follows normal restore and overlong guidance is rejected', async () => {
    service.request([chat('a')]);
    service.guidance.set('x'.repeat(4001));
    await service.confirm();
    expect(threads.setThreadArchived).not.toHaveBeenCalled();
    service.guidance.set('');
    await service.confirm();
    expect(threads.setThreadArchived).toHaveBeenCalledWith(chat('a'), false, '');
  });

  it('layout reset discards draft and stops remaining bulk operations', async () => {
    let resolve!: (value: boolean) => void;
    threads.setThreadArchived.mockReturnValueOnce(new Promise<boolean>(done => { resolve = done; }));
    service.request([chat('a'), chat('b')]);
    const pending = service.confirm();
    service.reset();
    resolve(true);
    await pending;
    expect(threads.setThreadArchived).toHaveBeenCalledTimes(1);
    expect(service.targets()).toEqual([]);
    expect(service.busy()).toBe(false);
  });
});

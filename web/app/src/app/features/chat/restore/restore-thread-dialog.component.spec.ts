import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';
import { ThreadListService } from '../../../core/services/thread-list.service';
import { RestoreThreadService } from './restore-thread.service';
import { RestoreThreadDialogComponent } from './restore-thread-dialog.component';

describe('RestoreThreadDialogComponent', () => {
  it('shows optional notes, confirms only on Unarchive, and closes without work on Cancel', async () => {
    const threads = { setThreadArchived: vi.fn().mockResolvedValue(true), setAllSelected: vi.fn(), error: signal(null) };
    TestBed.configureTestingModule({ imports: [RestoreThreadDialogComponent], providers: [{ provide: ThreadListService, useValue: threads }] });
    const fixture = TestBed.createComponent(RestoreThreadDialogComponent);
    const restore = TestBed.inject(RestoreThreadService);
    const target = { id: 'a', name: 'Travel plans', archived: true, user_id: 'owner', created_at: '', updated_at: '' };
    restore.request([target]);
    fixture.detectChanges();
    await fixture.whenStable();
    const root: HTMLElement = fixture.nativeElement;
    expect(root.querySelector('[role="dialog"]')?.getAttribute('aria-labelledby')).toBe('unarchive-title');
    expect(root.textContent).toContain('Travel plans');
    expect(root.textContent).toContain('existing memories remain available');
    const textarea = root.querySelector('textarea')!;
    expect(textarea.labels?.[0]?.textContent).toContain('optional');
    textarea.value = 'Keep the budget; omit abandoned plans.';
    textarea.dispatchEvent(new Event('input'));
    await fixture.whenStable();
    expect(restore.guidance()).toBe(textarea.value);
    const button = (label: string) => [...root.querySelectorAll('button')].find(item => item.textContent?.trim() === label)!;
    button('Cancel').click();
    fixture.detectChanges();
    expect(root.querySelector('[role="dialog"]')).toBeNull();
    expect(threads.setThreadArchived).not.toHaveBeenCalled();

    restore.request([target]);
    fixture.detectChanges();
    button('Unarchive').click();
    await fixture.whenStable();
    fixture.detectChanges();
    expect(threads.setThreadArchived).toHaveBeenCalledWith(target, false, '');
    expect(root.querySelector('[role="dialog"]')).toBeNull();
  });
});

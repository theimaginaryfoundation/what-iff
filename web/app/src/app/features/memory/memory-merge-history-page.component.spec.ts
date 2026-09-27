import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of } from 'rxjs';

import { MemoryMergeEvent } from '../../core/models/memory.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MemoryService } from '../../core/services/memory.service';
import { MemoryMergeHistoryPageComponent } from './memory-merge-history-page.component';

function makeEvent(partial: Partial<MemoryMergeEvent> = {}): MemoryMergeEvent {
  return {
    id: 'e-1',
    survivor_memory_id: 'm-1',
    merge_type: 'fold_live',
    content: 'Lives in New York',
    duplicates_folded: 1,
    source_members: [
      { content: 'Lives in NYC', scope: 'user', confidence: 'high', memory_id: 'm-2', is_new: true },
      { content: 'Lives in New York', scope: 'chat', memory_id: 'm-1', is_new: false },
    ],
    reverted_at: null,
    created_at: '2026-05-01T00:00:00Z',
    updated_at: '2026-05-01T00:00:00Z',
    ...partial,
  };
}

describe('MemoryMergeHistoryPageComponent', () => {
  let fixture: ComponentFixture<MemoryMergeHistoryPageComponent>;
  let memoryService: { listMergeEvents: ReturnType<typeof vi.fn>; undoMergeEvent: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };

  async function setup(events: MemoryMergeEvent[]): Promise<void> {
    memoryService = {
      listMergeEvents: vi.fn().mockReturnValue(of({ results: events, total_count: events.length, page: 1 })),
      undoMergeEvent: vi.fn().mockReturnValue(of(undefined)),
    };
    confirmation = { confirm: vi.fn().mockResolvedValue(false) };
    await TestBed.configureTestingModule({
      imports: [MemoryMergeHistoryPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: MemoryService, useValue: memoryService },
        { provide: ConfirmationService, useValue: confirmation },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(MemoryMergeHistoryPageComponent);
    fixture.detectChanges();
  }

  it('renders a help hint next to the title and accurate undo copy', async () => {
    await setup([]);
    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('h1 ui-help-hint button')?.getAttribute('aria-label')).toBe('What is merge history?');
    expect(host.textContent).toContain('merged duplicates stay archived');
    expect(host.textContent).not.toContain('Undo restores the prior state');
  });

  it('uses the shared merge-type label and plain scope/confidence labels', async () => {
    await setup([makeEvent()]);
    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('.merge-history-item__type')?.textContent?.trim()).toBe('Memories merged');

    fixture.componentInstance.toggleExpanded(fixture.componentInstance.events()[0]);
    fixture.detectChanges();
    const scopes = Array.from(host.querySelectorAll('.merge-history-source__scope')).map(el => el.textContent?.trim());
    expect(scopes).toEqual(['Global or personality', 'Thread']);
    expect(host.querySelector('.merge-history-source__confidence')?.textContent?.trim()).toBe('high confidence');
  });

  it('asks before undoing a create event and does nothing when cancelled', async () => {
    await setup([makeEvent({ merge_type: 'create' })]);
    await fixture.componentInstance.undo(fixture.componentInstance.events()[0]);

    expect(confirmation.confirm).toHaveBeenCalledWith(expect.objectContaining({ type: 'danger' }));
    expect(memoryService.undoMergeEvent).not.toHaveBeenCalled();
  });

  it('undoes a create event once confirmed', async () => {
    await setup([makeEvent({ merge_type: 'create' })]);
    confirmation.confirm.mockResolvedValue(true);
    await fixture.componentInstance.undo(fixture.componentInstance.events()[0]);

    expect(memoryService.undoMergeEvent).toHaveBeenCalledWith('e-1');
  });

  it('undoes a merge without a confirmation', async () => {
    await setup([makeEvent()]);
    await fixture.componentInstance.undo(fixture.componentInstance.events()[0]);

    expect(confirmation.confirm).not.toHaveBeenCalled();
    expect(memoryService.undoMergeEvent).toHaveBeenCalledWith('e-1');
  });
});

import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { MemoryCardVm } from '../helpers/memory-vm.helpers';
import { MemoryFocusPanelComponent } from './memory-focus-panel.component';

function makeVm(partial: Partial<MemoryCardVm> = {}): MemoryCardVm {
  return {
    id: 'm-1',
    content: 'Based in New York, not Chicago.',
    excerpt: 'Based in New York, not Chicago.',
    level: 'global',
    levelLabel: 'Global',
    status: 'active',
    starred: false,
    chatName: null,
    chatId: null,
    pinnedPersonalityId: null,
    pinnedPersonalityName: null,
    confidence: 0.45,
    confidencePercent: 45,
    confidenceLabel: 'Medium',
    verifiedCount: null,
    mergedFromIds: [],
    chainMetadata: null,
    createdAt: '2026-08-28T00:00:00Z',
    updatedAt: '2026-08-29T00:00:00Z',
    ...partial,
  };
}

describe('MemoryFocusPanelComponent', () => {
  let fixture: ComponentFixture<MemoryFocusPanelComponent>;
  let component: MemoryFocusPanelComponent;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [MemoryFocusPanelComponent],
      providers: [provideZonelessChangeDetection(), provideRouter([])],
    }).compileComponents();

    fixture = TestBed.createComponent(MemoryFocusPanelComponent);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('memory', makeVm());
    fixture.detectChanges();
  });

  it('renders memory content and confidence info control', () => {
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('Based in New York, not Chicago.');
    expect(text).toContain('45%');
    const info = fixture.nativeElement.querySelector('.focus-panel__info') as HTMLButtonElement;
    expect(info).toBeTruthy();
    expect(info.getAttribute('aria-label')).toBe('About confidence');
  });

  it('filters merge events to this survivor and skips reverted ones', () => {
    fixture.componentRef.setInput('mergeEvents', [
      {
        id: 'e-1',
        survivor_memory_id: 'm-1',
        merge_type: 'fold_live',
        content: 'Merged A',
        duplicates_folded: 1,
        created_at: '2026-08-20T00:00:00Z',
        updated_at: '2026-08-20T00:00:00Z',
        source_members: [{ content: 'A', scope: 'user', is_new: false, memory_id: 'm-a' }],
      },
      {
        id: 'e-2',
        survivor_memory_id: 'm-other',
        merge_type: 'fold_live',
        content: 'Other survivor',
        duplicates_folded: 1,
        created_at: '2026-08-21T00:00:00Z',
        updated_at: '2026-08-21T00:00:00Z',
      },
      {
        id: 'e-3',
        survivor_memory_id: 'm-1',
        merge_type: 'link',
        content: 'Reverted link',
        duplicates_folded: 0,
        reverted_at: '2026-08-22T00:00:00Z',
        created_at: '2026-08-21T00:00:00Z',
        updated_at: '2026-08-22T00:00:00Z',
      },
    ]);
    fixture.detectChanges();

    expect(component.relatedMergeEvents().map(e => e.id)).toEqual(['e-1']);
    expect(fixture.nativeElement.textContent).toContain('Memories merged');
    expect(fixture.nativeElement.textContent).not.toContain('Reverted link');
  });

  it('hides mutating actions when read-only', () => {
    fixture.componentRef.setInput('readOnly', true);
    fixture.detectChanges();

    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain("can't be archived or deleted here");
    expect(fixture.nativeElement.querySelector('.focus-panel__actions')).toBeNull();
    expect(fixture.nativeElement.querySelector('.focus-panel__link-btn')).toBeNull();
  });

  it('emits footer actions when editable', () => {
    const editSpy = vi.spyOn(component.edit, 'emit');
    const deleteSpy = vi.spyOn(component.delete, 'emit');
    const archiveSpy = vi.spyOn(component.archive, 'emit');

    const buttons = Array.from(
      fixture.nativeElement.querySelectorAll('.focus-panel__actions button') as NodeListOf<HTMLButtonElement>,
    );
    expect(buttons.map(b => b.textContent?.trim())).toEqual(['Edit', 'Archive', 'Delete']);

    buttons[0].click();
    buttons[1].click();
    buttons[2].click();
    expect(editSpy).toHaveBeenCalledWith('m-1');
    expect(archiveSpy).toHaveBeenCalledWith('m-1');
    expect(deleteSpy).toHaveBeenCalledWith('m-1');
  });

  it('hides its own close control when showCloseButton is false', () => {
    expect(fixture.nativeElement.querySelector('[aria-label="Close details"]')).toBeTruthy();

    fixture.componentRef.setInput('showCloseButton', false);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('[aria-label="Close details"]')).toBeNull();
    expect(fixture.nativeElement.querySelector('.focus-panel--flush')).toBeTruthy();
  });
});

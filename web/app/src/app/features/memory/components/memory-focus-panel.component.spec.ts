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

  it('emits starChange with the toggled payload from the header star button', () => {
    const starSpy = vi.spyOn(component.starChange, 'emit');
    const starButton = () =>
      fixture.nativeElement.querySelector('.focus-panel__icon-btn[aria-label="Star"]') as HTMLButtonElement | null;

    expect(starButton()).toBeTruthy();
    starButton()!.click();
    expect(starSpy).toHaveBeenCalledWith({ id: 'm-1', starred: true });

    fixture.componentRef.setInput('memory', makeVm({ starred: true }));
    fixture.detectChanges();

    const unstarButton = fixture.nativeElement.querySelector(
      '.focus-panel__icon-btn[aria-label="Unstar"]',
    ) as HTMLButtonElement;
    expect(unstarButton).toBeTruthy();
    expect(starButton()).toBeNull();
    unstarButton.click();
    expect(starSpy).toHaveBeenCalledWith({ id: 'm-1', starred: false });
  });

  it('hides the header star button when read-only', () => {
    fixture.componentRef.setInput('readOnly', true);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.focus-panel__icon-btn[aria-label="Star"]')).toBeNull();
    expect(fixture.nativeElement.querySelector('.focus-panel__icon-btn[aria-label="Unstar"]')).toBeNull();
  });

  it('renders the Starred badge only when the memory is starred', () => {
    expect(fixture.nativeElement.querySelector('.focus-panel__starred')).toBeNull();

    fixture.componentRef.setInput('memory', makeVm({ starred: true }));
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.focus-panel__starred')?.textContent?.trim()).toBe('Starred');
  });

  it('renders the pinned personality cover instead of the scope chip when the id resolves', () => {
    fixture.componentRef.setInput('personalities', [
      { id: 'p-1', label: 'Nova', accent_color: '#123456', cover_image_url: null, thumbnail_circle: null },
    ]);
    fixture.componentRef.setInput('memory', makeVm({ pinnedPersonalityId: 'p-1', pinnedPersonalityName: 'Nova' }));
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('persona-accent-scope')).toBeTruthy();
    expect(fixture.nativeElement.querySelector('persona-cover')).toBeTruthy();
    expect(fixture.nativeElement.querySelector('.focus-panel__scope-chip')).toBeNull();
    expect(fixture.nativeElement.textContent).toContain('Nova');
  });

  it('falls back to the scope chip when the pinned personality id is not found in the list', () => {
    fixture.componentRef.setInput('personalities', []);
    fixture.componentRef.setInput('memory', makeVm({ pinnedPersonalityId: 'p-missing', pinnedPersonalityName: 'Nova' }));
    fixture.detectChanges();

    expect(component.pinnedPersonality()).toBeNull();
    expect(fixture.nativeElement.querySelector('persona-accent-scope')).toBeNull();
    expect(fixture.nativeElement.querySelector('.focus-panel__scope-chip')).toBeTruthy();
  });

  it('emits move with the memory id from the Move button', () => {
    const moveSpy = vi.spyOn(component.move, 'emit');
    const moveButton = fixture.nativeElement.querySelector('.focus-panel__link-btn') as HTMLButtonElement;

    moveButton.click();

    expect(moveSpy).toHaveBeenCalledWith('m-1');
  });

  it('shows the confidence tooltip with the exact copy on hover/focus', () => {
    const infoButton = fixture.nativeElement.querySelector('.focus-panel__info') as HTMLButtonElement;

    infoButton.dispatchEvent(new Event('mouseenter'));
    fixture.detectChanges();

    const tooltip = document.body.querySelector('[role="tooltip"]');
    expect(tooltip?.textContent).toBe('Your personalities rate their confidence when writing and updating memories');
  });

  it('still shows the confidence tooltip under a simulated coarse (touch) pointer, since disabledOnTouch is false', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({
      matches: true,
      media: '(pointer: coarse)',
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => false,
    } as MediaQueryList);

    const infoButton = fixture.nativeElement.querySelector('.focus-panel__info') as HTMLButtonElement;
    infoButton.dispatchEvent(new Event('focus'));
    fixture.detectChanges();

    const tooltip = document.body.querySelector('[role="tooltip"]');
    expect(tooltip?.textContent).toContain('confidence');
  });

  it('shows the verified count when set and omits it when null', () => {
    expect(fixture.nativeElement.textContent).not.toContain('verified');

    fixture.componentRef.setInput('memory', makeVm({ verifiedCount: 3 }));
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('verified 3×');
  });

  it('hides the merged-from section when there are no merged ids', () => {
    expect(fixture.nativeElement.querySelector('.focus-panel__id-list')).toBeNull();
  });

  it('renders a truncated, linked entry per merged-from id', () => {
    fixture.componentRef.setInput('memory', makeVm({ mergedFromIds: ['abcdef1234567890', 'zzyyxxwwvvuu'] }));
    fixture.detectChanges();

    const links = Array.from(
      fixture.nativeElement.querySelectorAll('.focus-panel__id-list li a') as NodeListOf<HTMLAnchorElement>,
    );
    expect(links).toHaveLength(2);
    expect(links[0].textContent?.trim()).toBe('abcdef12…');
    expect(links[0].getAttribute('href')).toBe('/memories/abcdef1234567890');
    expect(links[1].getAttribute('href')).toBe('/memories/zzyyxxwwvvuu');
  });

  it('shows a loading message while merge events are loading', () => {
    fixture.componentRef.setInput('mergeEventsLoading', true);
    fixture.detectChanges();

    expect(fixture.nativeElement.textContent).toContain('Loading merge events…');
  });

  it('shows an empty state with a link to merge history when there are no related events', () => {
    const text = fixture.nativeElement.textContent as string;
    expect(text).toContain('No merge events for this memory.');

    const link = fixture.nativeElement.querySelector('a[href^="/memories"]') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toBe('/memories?tab=merge-history');
  });

  it('labels a "link" merge event as linking related memories', () => {
    fixture.componentRef.setInput('mergeEvents', [
      {
        id: 'e-1',
        survivor_memory_id: 'm-1',
        merge_type: 'link',
        content: 'Linked stuff',
        duplicates_folded: 0,
        created_at: '2026-08-20T00:00:00Z',
        updated_at: '2026-08-20T00:00:00Z',
      },
    ]);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.focus-panel__event-top strong')?.textContent).toBe(
      'Linked related memories',
    );
  });

  it('labels a "create" merge event as created from batch', () => {
    fixture.componentRef.setInput('mergeEvents', [
      {
        id: 'e-1',
        survivor_memory_id: 'm-1',
        merge_type: 'create',
        content: 'Batch created',
        duplicates_folded: 0,
        created_at: '2026-08-20T00:00:00Z',
        updated_at: '2026-08-20T00:00:00Z',
      },
    ]);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.focus-panel__event-top strong')?.textContent).toBe(
      'Created from batch',
    );
  });

  it('falls back to duplicates_folded + 1 for the source count when there are no source_members', () => {
    fixture.componentRef.setInput('mergeEvents', [
      {
        id: 'e-1',
        survivor_memory_id: 'm-1',
        merge_type: 'fold_live',
        content: 'Merged A',
        duplicates_folded: 2,
        created_at: '2026-08-20T00:00:00Z',
        updated_at: '2026-08-20T00:00:00Z',
      },
    ]);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.focus-panel__event p')?.textContent).toContain('3 sources');
  });

  it('clamps the fallback source count at 0 rather than going negative', () => {
    fixture.componentRef.setInput('mergeEvents', [
      {
        id: 'e-1',
        survivor_memory_id: 'm-1',
        merge_type: 'fold_live',
        content: 'Merged A',
        duplicates_folded: -1,
        created_at: '2026-08-20T00:00:00Z',
        updated_at: '2026-08-20T00:00:00Z',
      },
    ]);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.focus-panel__event p')?.textContent).toContain('0 sources');
  });

  it('renders linked source members as links and unlinked ones as plain text', () => {
    fixture.componentRef.setInput('mergeEvents', [
      {
        id: 'e-1',
        survivor_memory_id: 'm-1',
        merge_type: 'fold_live',
        content: 'Merged A',
        duplicates_folded: 1,
        created_at: '2026-08-20T00:00:00Z',
        updated_at: '2026-08-20T00:00:00Z',
        source_members: [
          { content: 'Linked member', scope: 'user', is_new: false, memory_id: 'm-a' },
          { content: 'Plain member', scope: 'user', is_new: true },
        ],
      },
    ]);
    fixture.detectChanges();

    const members = Array.from(
      fixture.nativeElement.querySelectorAll('.focus-panel__members li') as NodeListOf<HTMLLIElement>,
    );
    expect(members).toHaveLength(2);

    const link = members[0].querySelector('a') as HTMLAnchorElement;
    expect(link.textContent).toBe('Linked member');
    expect(link.getAttribute('href')).toBe('/memories/m-a');

    expect(members[1].querySelector('a')).toBeNull();
    expect(members[1].querySelector('span')?.textContent).toBe('Plain member');
  });
});

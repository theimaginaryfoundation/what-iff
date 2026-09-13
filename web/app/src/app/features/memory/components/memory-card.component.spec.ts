import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { MemoryCardVm } from '../helpers/memory-vm.helpers';
import { MemoryCardComponent } from './memory-card.component';

function makeVm(partial: Partial<MemoryCardVm> = {}): MemoryCardVm {
  return {
    id: 'm-1',
    content: 'Full memory content',
    excerpt: 'Full memory content',
    level: 'thread',
    levelLabel: 'Thread',
    status: 'active',
    starred: false,
    chatName: 'Thread A',
    chatId: 'c-1',
    pinnedPersonalityId: null,
    pinnedPersonalityName: null,
    confidence: 0.6,
    confidencePercent: 60,
    confidenceLabel: 'Medium',
    verifiedCount: null,
    mergedFromIds: [],
    chainMetadata: null,
    createdAt: '2026-05-01T00:00:00Z',
    updatedAt: '2026-05-01T00:00:00Z',
    ...partial,
  };
}

describe('MemoryCardComponent', () => {
  let fixture: ComponentFixture<MemoryCardComponent>;
  let component: MemoryCardComponent;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [MemoryCardComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(MemoryCardComponent);
    component = fixture.componentInstance;
    fixture.componentRef.setInput('memory', {
      id: 'm-1',
      content: 'Full memory content',
      excerpt: 'Full memory content',
      level: 'thread',
      levelLabel: 'Thread',
      status: 'active',
      starred: false,
      chatName: 'Thread A',
      chatId: 'c-1',
      pinnedPersonalityId: null,
      pinnedPersonalityName: null,
      confidence: 0.6,
      confidencePercent: 60,
      confidenceLabel: 'Medium',
      verifiedCount: null,
      mergedFromIds: [],
      chainMetadata: null,
      createdAt: '2026-05-01T00:00:00Z',
      updatedAt: '2026-05-01T00:00:00Z',
    });
    fixture.detectChanges();
  });

  it('renders content as plain text', () => {
    const content = fixture.nativeElement.querySelector('.memory-card__content');
    expect(content.tagName.toLowerCase()).toBe('p');
  });

  it('hides checkbox and action menu when read-only', () => {
    fixture.componentRef.setInput('readOnly', true);
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('.memory-card__checkbox')).toBeNull();
    expect(fixture.nativeElement.querySelector('.memory-card__menu-btn')).toBeNull();
    expect(fixture.nativeElement.textContent).toContain('Thread checkpoint summary');
  });

  it('emits focus when the card body is activated', () => {
    const focusSpy = vi.spyOn(component.focus, 'emit');
    const card = fixture.nativeElement.querySelector('.memory-card') as HTMLElement;
    card.click();
    expect(focusSpy).toHaveBeenCalledWith('m-1');
  });

  it('emits save when inline edit is submitted', () => {
    const saveSpy = vi.spyOn(component.save, 'emit').mockReturnValue(undefined);
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();
    const editButton = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Edit');
    expect(editButton).toBeTruthy();
    editButton!.click();
    fixture.detectChanges();
    const textarea = fixture.nativeElement.querySelector('.memory-card__textarea') as HTMLTextAreaElement;
    textarea.value = 'Updated memory content';
    textarea.dispatchEvent(new Event('input'));
    // ngModel listens to input via FormsModule; also update the signal path used by the template
    component.draft.set('Updated memory content');
    fixture.detectChanges();
    const saveButton = fixture.nativeElement.querySelector('.memory-card__save') as HTMLButtonElement;
    saveButton.click();
    expect(saveSpy).toHaveBeenCalledWith({ id: 'm-1', content: 'Updated memory content' });
  });

  it('toggles the action menu and its aria-expanded state without leaking a focus event', () => {
    const focusSpy = vi.spyOn(component.focus, 'emit');
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    expect(menuButton.getAttribute('aria-expanded')).toBe('false');
    expect(fixture.nativeElement.querySelector('.memory-card__menu[role="menu"]')).toBeNull();

    menuButton.click();
    fixture.detectChanges();
    expect(menuButton.getAttribute('aria-expanded')).toBe('true');
    expect(fixture.nativeElement.querySelector('.memory-card__menu[role="menu"]')).toBeTruthy();
    expect(focusSpy).not.toHaveBeenCalled();

    menuButton.click();
    fixture.detectChanges();
    expect(menuButton.getAttribute('aria-expanded')).toBe('false');
    expect(fixture.nativeElement.querySelector('.memory-card__menu[role="menu"]')).toBeNull();
  });

  it('renders the backdrop only while the menu is open, and closes the menu when clicked', () => {
    expect(fixture.nativeElement.querySelector('.memory-card__backdrop')).toBeNull();

    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const backdrop = fixture.nativeElement.querySelector('.memory-card__backdrop') as HTMLButtonElement;
    expect(backdrop).toBeTruthy();
    backdrop.click();
    fixture.detectChanges();

    expect(component.menuOpen()).toBe(false);
    expect(fixture.nativeElement.querySelector('.memory-card__backdrop')).toBeNull();
  });

  it('never renders the backdrop when read-only, even if the menu were somehow open', () => {
    fixture.componentRef.setInput('readOnly', true);
    component.menuOpen.set(true);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.memory-card__backdrop')).toBeNull();
  });

  it('shows Star in the menu and emits starred:true, closing the menu', () => {
    const starSpy = vi.spyOn(component.starChange, 'emit');
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const starItem = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button[role="menuitem"]') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Star');
    expect(starItem).toBeTruthy();
    starItem!.click();

    expect(starSpy).toHaveBeenCalledWith({ id: 'm-1', starred: true });
    expect(component.menuOpen()).toBe(false);
  });

  it('shows Unstar in the menu when already starred and emits starred:false', () => {
    fixture.componentRef.setInput('memory', makeVm({ starred: true }));
    fixture.detectChanges();
    const starSpy = vi.spyOn(component.starChange, 'emit');
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const starItem = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button[role="menuitem"]') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Unstar');
    expect(starItem).toBeTruthy();
    starItem!.click();

    expect(starSpy).toHaveBeenCalledWith({ id: 'm-1', starred: false });
  });

  it('emits move and closes the menu from the Move menu item', () => {
    const moveSpy = vi.spyOn(component.move, 'emit');
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const moveItem = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button[role="menuitem"]') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Move');
    moveItem!.click();

    expect(moveSpy).toHaveBeenCalledWith('m-1');
    expect(component.menuOpen()).toBe(false);
  });

  it('labels the archive menu item Archive by default and emits archive', () => {
    const archiveSpy = vi.spyOn(component.archive, 'emit');
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const archiveItem = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button[role="menuitem"]') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Archive');
    expect(archiveItem).toBeTruthy();
    archiveItem!.click();

    expect(archiveSpy).toHaveBeenCalledWith('m-1');
  });

  it('labels the archive menu item Unarchive in the archived view', () => {
    fixture.componentRef.setInput('isArchivedView', true);
    fixture.detectChanges();
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const archiveItem = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button[role="menuitem"]') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Unarchive');
    expect(archiveItem).toBeTruthy();
  });

  it('emits delete from the danger Delete menu item', () => {
    const deleteSpy = vi.spyOn(component.delete, 'emit');
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();

    const deleteItem = fixture.nativeElement.querySelector('.memory-card__menu-danger') as HTMLButtonElement;
    expect(deleteItem.textContent?.trim()).toBe('Delete');
    deleteItem.click();

    expect(deleteSpy).toHaveBeenCalledWith('m-1');
  });

  it('emits toggleSelect via checkbox click without leaking a card focus event', () => {
    const toggleSpy = vi.spyOn(component.toggleSelect, 'emit');
    const focusSpy = vi.spyOn(component.focus, 'emit');
    const checkbox = fixture.nativeElement.querySelector('.memory-card__checkbox') as HTMLInputElement;

    checkbox.click();
    fixture.detectChanges();

    expect(toggleSpy).toHaveBeenCalledWith('m-1');
    expect(focusSpy).not.toHaveBeenCalled();
  });

  it('reflects the selected input on the checkbox and the host selected class', () => {
    fixture.componentRef.setInput('selected', true);
    fixture.detectChanges();

    const checkbox = fixture.nativeElement.querySelector('.memory-card__checkbox') as HTMLInputElement;
    expect(checkbox.checked).toBe(true);
    expect(fixture.nativeElement.querySelector('.memory-card')?.classList.contains('memory-card--selected')).toBe(
      true,
    );
  });

  it('reflects the focused input as a host class', () => {
    expect(fixture.nativeElement.querySelector('.memory-card')?.classList.contains('memory-card--focused')).toBe(
      false,
    );

    fixture.componentRef.setInput('focused', true);
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.memory-card')?.classList.contains('memory-card--focused')).toBe(
      true,
    );
  });

  it('ignores card activation when the event target is an interactive descendant', () => {
    const focusSpy = vi.spyOn(component.focus, 'emit');
    const input = document.createElement('input');

    component.onCardActivate({ target: input } as unknown as Event);

    expect(focusSpy).not.toHaveBeenCalled();
  });

  it('ignores card activation while editing, even when the target is the card itself', () => {
    const focusSpy = vi.spyOn(component.focus, 'emit');
    component.startEdit();
    fixture.detectChanges();

    const card = fixture.nativeElement.querySelector('.memory-card') as HTMLElement;
    card.click();

    expect(focusSpy).not.toHaveBeenCalled();
  });

  it('reverts the draft and closes the editor on cancel', () => {
    const menuButton = fixture.nativeElement.querySelector('.memory-card__menu-btn') as HTMLButtonElement;
    menuButton.click();
    fixture.detectChanges();
    const editButton = Array.from(
      fixture.nativeElement.querySelectorAll('.memory-card__menu button') as NodeListOf<HTMLButtonElement>,
    ).find(btn => btn.textContent?.trim() === 'Edit');
    editButton!.click();
    fixture.detectChanges();

    component.draft.set('Mutated but not saved');
    fixture.detectChanges();

    const cancelButton = fixture.nativeElement.querySelector('.memory-card__cancel') as HTMLButtonElement;
    cancelButton.click();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.memory-card__editor')).toBeNull();
    expect(fixture.nativeElement.querySelector('.memory-card__content')?.textContent?.trim()).toBe(
      'Full memory content',
    );
    expect(component.editing()).toBe(false);
  });

  it('disables Save when the draft is empty or whitespace, and submitEdit is a no-op', () => {
    component.startEdit();
    component.draft.set('   ');
    fixture.detectChanges();

    const saveButton = fixture.nativeElement.querySelector('.memory-card__save') as HTMLButtonElement;
    expect(saveButton.disabled).toBe(true);

    const saveSpy = vi.spyOn(component.save, 'emit');
    component.submitEdit();
    expect(saveSpy).not.toHaveBeenCalled();
    expect(component.editing()).toBe(true);
  });

  it('hides the pin control for thread and summary levels even while editing', () => {
    component.startEdit();
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.memory-card__pin-select')).toBeNull();
  });

  it('renders one pin option per personality plus "All personalities", pre-set to the current pin', async () => {
    fixture.componentRef.setInput('memory', makeVm({ level: 'global', pinnedPersonalityId: 'p-2' }));
    fixture.componentRef.setInput('personalities', [
      { id: 'p-1', label: 'Nova' },
      { id: 'p-2', label: 'Echo' },
    ]);
    fixture.detectChanges();
    component.startEdit();
    fixture.detectChanges();
    // The select's initial selection is applied via SelectControlValueAccessor's
    // afterNextRender-deferred writeValue, not synchronously within detectChanges().
    await fixture.whenStable();

    const select = fixture.nativeElement.querySelector('.memory-card__pin-select') as HTMLSelectElement;
    expect(select).toBeTruthy();
    const optionLabels = Array.from(select.options).map(o => o.textContent?.trim());
    expect(optionLabels).toEqual(['All personalities', 'Nova', 'Echo']);
    expect(select.options[select.selectedIndex].textContent?.trim()).toBe('Echo');
  });

  it('disables the pin select while a pin update is in flight', async () => {
    fixture.componentRef.setInput('memory', makeVm({ level: 'personality' }));
    fixture.componentRef.setInput('pinUpdating', true);
    fixture.detectChanges();
    component.startEdit();
    fixture.detectChanges();
    // NgModel routes the `disabled` binding through its own FormControl and
    // applies it via a resolved-promise microtask, not synchronously in detectChanges().
    await fixture.whenStable();

    const select = fixture.nativeElement.querySelector('.memory-card__pin-select') as HTMLSelectElement;
    expect(select.disabled).toBe(true);
  });

  it('emits pinChange with the selected personality id and updates pinDraft', () => {
    fixture.componentRef.setInput('memory', makeVm({ level: 'global', pinnedPersonalityId: null }));
    fixture.detectChanges();
    const pinSpy = vi.spyOn(component.pinChange, 'emit');

    component.onPinChange('p-1');

    expect(pinSpy).toHaveBeenCalledWith({ id: 'm-1', pinnedPersonalityId: 'p-1' });
    expect(component.pinDraft()).toBe('p-1');
  });

  it('does not enter edit mode when startEdit is called while read-only', () => {
    fixture.componentRef.setInput('readOnly', true);
    fixture.detectChanges();

    component.startEdit();

    expect(component.editing()).toBe(false);
  });

  it.each([
    { level: 'global' as const, label: 'Global' },
    { level: 'personality' as const, label: 'Personality' },
    { level: 'thread' as const, label: 'Thread' },
    { level: 'summary' as const, label: 'Summary' },
  ])('renders the fallback avatar with data-level=$level for a $label memory', ({ level }) => {
    fixture.componentRef.setInput('memory', makeVm({ level }));
    fixture.detectChanges();

    const fallback = fixture.nativeElement.querySelector('.memory-card__avatar-fallback') as HTMLElement;
    expect(fallback).toBeTruthy();
    expect(fallback.getAttribute('data-level')).toBe(level);
    expect(fixture.nativeElement.querySelector('ui-globe-icon')).toBeTruthy();
  });

  it('renders the persona cover instead of the fallback avatar when the pinned personality is found', () => {
    fixture.componentRef.setInput('personalities', [{ id: 'p-1', label: 'Nova' }]);
    fixture.componentRef.setInput('memory', makeVm({ pinnedPersonalityId: 'p-1', pinnedPersonalityName: 'Nova' }));
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('persona-accent-scope')).toBeTruthy();
    expect(fixture.nativeElement.querySelector('persona-cover')).toBeTruthy();
    expect(fixture.nativeElement.querySelector('.memory-card__avatar-fallback')).toBeNull();
  });

  it('shows the confidence percent and label in the footer when editable', () => {
    expect(fixture.nativeElement.querySelector('.memory-card__footer')?.textContent).toContain(
      '60% · Medium confidence',
    );
  });

  it('shows the verified-count badge with its title when set, and hides it when null', () => {
    expect(fixture.nativeElement.querySelector('.memory-card__verified')).toBeNull();

    fixture.componentRef.setInput('memory', makeVm({ verifiedCount: 3 }));
    fixture.detectChanges();

    const badge = fixture.nativeElement.querySelector('.memory-card__verified') as HTMLElement;
    expect(badge.textContent?.trim()).toBe('3×');
    expect(badge.getAttribute('title')).toBe('Verified duplicate count');
  });

  it('shows the star badge in the top row when starred', () => {
    expect(fixture.nativeElement.querySelector('.memory-card__star')).toBeNull();

    fixture.componentRef.setInput('memory', makeVm({ starred: true }));
    fixture.detectChanges();

    expect(fixture.nativeElement.querySelector('.memory-card__star')).toBeTruthy();
  });
});

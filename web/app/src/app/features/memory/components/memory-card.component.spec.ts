import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { MemoryCardComponent } from './memory-card.component';

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
});

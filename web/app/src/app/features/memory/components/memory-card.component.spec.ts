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
            levelLabel: 'Chat',
            chatName: 'Thread A',
            pinnedPersonalityId: null,
            createdAt: '2026-05-01T00:00:00Z',
            updatedAt: '2026-05-01T00:00:00Z',
        });
        fixture.detectChanges();
    });

    it('does not render content as clickable button', () => {
        const button = fixture.nativeElement.querySelector('.memory-card__excerpt');
        expect(button.tagName.toLowerCase()).toBe('p');
    });

    it('emits save when inline edit is submitted', () => {
        const saveSpy = vi.spyOn(component.save, 'emit').mockReturnValue(undefined);
        const editButton = fixture.nativeElement.querySelector('.memory-card__icon-button') as HTMLButtonElement;
        editButton.click();
        fixture.detectChanges();
        const textarea = fixture.nativeElement.querySelector('.memory-card__textarea') as HTMLTextAreaElement;
        textarea.value = 'Updated memory content';
        textarea.dispatchEvent(new Event('input'));
        fixture.detectChanges();
        const saveButton = fixture.nativeElement.querySelector('.memory-card__save') as HTMLButtonElement;
        saveButton.click();
        expect(saveSpy).toHaveBeenCalledWith({ id: 'm-1', content: 'Updated memory content' });
    });
});

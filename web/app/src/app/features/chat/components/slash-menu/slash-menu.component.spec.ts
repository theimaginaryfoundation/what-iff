import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { SlashMenuComponent } from './slash-menu.component';

describe('SlashMenuComponent', () => {
    let fixture: ComponentFixture<SlashMenuComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [SlashMenuComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(SlashMenuComponent);
        fixture.componentRef.setInput('open', true);
        fixture.componentRef.setInput('commands', [
            { id: 'attach', label: 'Attach', description: 'Attach a file' },
            { id: 'ritual', label: 'Ritual', description: 'Run a ritual' },
        ]);
        fixture.detectChanges();
    });

    it('moves selection with arrows and selects with enter', () => {
        const spy = vi.fn().mockName('selected');
        fixture.componentInstance.selected.subscribe(spy);

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Enter' }));

        expect(spy).toHaveBeenCalledWith(expect.objectContaining({ id: 'ritual' }));
    });

    it('emits closed on escape', () => {
        const spy = vi.fn().mockName('closed');
        fixture.componentInstance.closed.subscribe(spy);

        fixture.componentInstance.onKeydown(new KeyboardEvent('keydown', { key: 'Escape' }));

        expect(spy).toHaveBeenCalled();
    });
});

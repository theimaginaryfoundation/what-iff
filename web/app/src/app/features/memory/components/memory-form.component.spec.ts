import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { MemoryFormComponent } from './memory-form.component';

describe('MemoryFormComponent', () => {
    let fixture: ComponentFixture<MemoryFormComponent>;
    let component: MemoryFormComponent;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [MemoryFormComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();
        fixture = TestBed.createComponent(MemoryFormComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('memory', {
            id: 'm-1',
            content: 'hello world',
            level: 'thread',
            type: 'Context',
            starred: false,
            created_at: '2026-05-01T00:00:00Z',
            updated_at: '2026-05-01T00:00:00Z',
        });
        fixture.detectChanges();
    });

    it('emits save payload with updated fields', () => {
        const spy = vi.spyOn(component.save, 'emit').mockReturnValue(undefined);
        component.content.set('updated');
        component.level.set('summary');
        component.onSubmit();
        expect(spy).toHaveBeenCalledWith({ content: 'updated', level: 'summary' });
    });

    it('emits pinChange when personality pin is updated', () => {
        fixture.componentRef.setInput('memory', {
            id: 'm-1',
            content: 'hello world',
            level: 'global',
            type: 'Context',
            starred: false,
            created_at: '2026-05-01T00:00:00Z',
            updated_at: '2026-05-01T00:00:00Z',
        });
        fixture.componentRef.setInput('personalities', [{ id: 'p-1', label: 'Vera' }]);
        fixture.detectChanges();

        const pinSpy = vi.spyOn(component.pinChange, 'emit').mockReturnValue(undefined);
        component.onPinnedPersonalityChange('p-1');
        expect(pinSpy).toHaveBeenCalledWith('p-1');
    });
});

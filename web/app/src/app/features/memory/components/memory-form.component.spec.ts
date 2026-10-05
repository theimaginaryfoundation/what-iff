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
        expect(spy).toHaveBeenCalledWith({ content: 'updated', level: 'summary', sensitivity: 'personal' });
    });

    it('defaults the sensitivity selector to Personal for a memory without one', () => {
        const select = fixture.nativeElement.querySelector('select[name="memory-sensitivity"]') as HTMLSelectElement;
        expect(Array.from(select.options).map(o => o.textContent?.trim())).toEqual(['Public', 'Personal', 'Sensitive']);
        expect(component.sensitivity()).toBe('personal');
    });

    it('loads the memory sensitivity into the selector', () => {
        fixture.componentRef.setInput('memory', {
            id: 'm-1',
            content: 'hello world',
            level: 'thread',
            type: 'Context',
            status: 'active',
            sensitivity: 'sensitive',
            confidence: 0.6,
            starred: false,
            created_at: '2026-05-01T00:00:00Z',
            updated_at: '2026-05-01T00:00:00Z',
        });
        fixture.detectChanges();

        expect(component.sensitivity()).toBe('sensitive');
    });

    it('emits the edited sensitivity with the other fields on save', () => {
        const spy = vi.spyOn(component.save, 'emit').mockReturnValue(undefined);
        component.sensitivity.set('public');
        component.onSubmit();
        expect(spy).toHaveBeenCalledWith({ content: 'hello world', level: 'thread', sensitivity: 'public' });
    });

    it('shows the explanation for the chosen level', () => {
        component.sensitivity.set('sensitive');
        fixture.detectChanges();
        const hint = fixture.nativeElement.querySelector('#memory-sensitivity-hint') as HTMLElement;
        expect(hint.textContent).toContain('full memory access');
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

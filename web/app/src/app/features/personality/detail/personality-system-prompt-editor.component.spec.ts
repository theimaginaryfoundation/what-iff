import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { PersonalitySystemPromptEditorComponent, SystemPromptValue } from './personality-system-prompt-editor.component';

describe('PersonalitySystemPromptEditorComponent', () => {
    let fixture: ComponentFixture<PersonalitySystemPromptEditorComponent>;
    let component: PersonalitySystemPromptEditorComponent;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [PersonalitySystemPromptEditorComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();
        fixture = TestBed.createComponent(PersonalitySystemPromptEditorComponent);
        component = fixture.componentInstance;
    });

    function setValue(value: SystemPromptValue): void {
        fixture.componentRef.setInput('value', value);
        fixture.detectChanges();
    }

    it('renders the value in read-only mode', () => {
        setValue({ name: 'Vera', systemPrompt: 'Spectral guide' });
        const text = fixture.nativeElement.textContent;
        expect(text).toContain('Vera');
        expect(text).toContain('Spectral guide');
    });

    it('emits save when valid changes are submitted', () => {
        setValue({ name: 'Vera', systemPrompt: 'old prompt' });
        component.startEdit();
        component.setName('Vera Calder');
        component.setSystemPrompt('updated prompt');
        const events: SystemPromptValue[] = [];
        component.save.subscribe(value => events.push(value));
        component.onSave();
        expect(events).toEqual([{ name: 'Vera Calder', systemPrompt: 'updated prompt' }]);
    });

    it('blocks save when name or prompt is empty', () => {
        setValue({ name: 'Vera', systemPrompt: 'prompt' });
        component.startEdit();
        component.setName('   ');
        expect(component.canSave()).toBe(false);
    });

    it('blocks save when prompt exceeds the hard limit', () => {
        setValue({ name: 'Vera', systemPrompt: 'prompt' });
        component.startEdit();
        component.setSystemPrompt('x'.repeat(component.hardLimit + 1));
        expect(component.canSave()).toBe(false);
    });
});

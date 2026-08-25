import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { ModeEditModalComponent, ModeEditModalState } from './mode-edit-modal.component';

describe('ModeEditModalComponent', () => {
    function makeState(overrides: Partial<ModeEditModalState> = {}): ModeEditModalState {
        return {
            mode: 'edit',
            open: true,
            loading: false,
            error: null,
            saving: false,
            name: 'Focused',
            description: 'desc',
            promptSnippet: 'inject this mode prompt',
            recommendedModel: 'model-1',
            attachedSkills: [{ id: 'r1', label: 'Memory Search' }],
            attachedMCPServers: [{ id: 'mcp-1', label: 'Stripe MCP' }],
            ...overrides,
        };
    }

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ModeEditModalComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();
    });

    it('emits form and action events', () => {
        const fixture = TestBed.createComponent(ModeEditModalComponent);
        fixture.componentRef.setInput('state', makeState());
        fixture.componentRef.setInput('modelOptions', [{ id: 'model-1', label: 'GPT-5' }]);
        fixture.componentRef.setInput('skillPickerOptions', [{ id: 'r2', label: 'Critique' }]);
        fixture.componentRef.setInput('skillQuery', '');
        fixture.componentRef.setInput('skillDropdownOpen', false);
        fixture.componentRef.setInput('attachedSkillsCountLabel', '1 attached');
        fixture.componentRef.setInput('attachedMcpCountLabel', '1 attached');

        let nameValue = '';
        let promptSnippetValue = '';
        let didSave = false;
        let didDelete = false;
        fixture.componentInstance.nameChange.subscribe(value => {
            nameValue = value;
        });
        fixture.componentInstance.promptSnippetChange.subscribe(value => {
            promptSnippetValue = value;
        });
        fixture.componentInstance.save.subscribe(() => {
            didSave = true;
        });
        fixture.componentInstance.deleteMode.subscribe(() => {
            didDelete = true;
        });

        fixture.detectChanges();

        const nameInput = fixture.nativeElement.querySelector('.mode-modal__name-input');
        nameInput.value = 'Deep Focus';
        nameInput.dispatchEvent(new Event('input'));
        const promptSnippetInput = fixture.nativeElement.querySelector('textarea[placeholder*="system prompt"]');
        promptSnippetInput.value = 'Keep responses concise.';
        promptSnippetInput.dispatchEvent(new Event('input'));

        const saveButton = fixture.nativeElement.querySelector('.mode-btn--primary');
        const deleteButton = fixture.nativeElement.querySelector('.mode-btn--danger');
        saveButton.click();
        deleteButton.click();

        expect(nameValue).toBe('Deep Focus');
        expect(promptSnippetValue).toBe('Keep responses concise.');
        expect(didSave).toBe(true);
        expect(didDelete).toBe(true);
    });

    it('routes backdrop click to dismissRequested and the close button to close', () => {
        const fixture = TestBed.createComponent(ModeEditModalComponent);
        fixture.componentRef.setInput('state', makeState());
        fixture.componentRef.setInput('modelOptions', []);
        fixture.componentRef.setInput('skillPickerOptions', []);
        fixture.componentRef.setInput('skillQuery', '');
        fixture.componentRef.setInput('skillDropdownOpen', false);
        fixture.componentRef.setInput('attachedSkillsCountLabel', '1 attached');
        fixture.componentRef.setInput('attachedMcpCountLabel', '1 attached');

        let closeCount = 0;
        let dismissRequestedCount = 0;
        fixture.componentInstance.close.subscribe(() => (closeCount += 1));
        fixture.componentInstance.dismissRequested.subscribe(() => (dismissRequestedCount += 1));
        fixture.detectChanges();

        (fixture.nativeElement.querySelector('.mode-modal-backdrop') as HTMLElement).click();
        expect(dismissRequestedCount).toBe(1);
        expect(closeCount).toBe(0);

        (fixture.nativeElement.querySelector('.mode-modal__close-btn') as HTMLButtonElement).click();
        expect(closeCount).toBe(1);
        expect(dismissRequestedCount).toBe(1);

        fixture.componentInstance.onEscape();
        expect(dismissRequestedCount).toBe(2);
    });
});

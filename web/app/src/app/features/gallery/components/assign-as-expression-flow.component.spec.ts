import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ExpressionAssignmentService } from '../../../core/services/expression-assignment.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { AssignAsExpressionFlowComponent } from './assign-as-expression-flow.component';

describe('AssignAsExpressionFlowComponent', () => {
    let personalityService: { listExpressions: ReturnType<typeof vi.fn> };
    let assignmentService: { assignFromGallery: ReturnType<typeof vi.fn> };

    beforeEach(async () => {
        personalityService = {
            listExpressions: vi.fn().mockName("PersonalityService.listExpressions")
        };
        assignmentService = {
            assignFromGallery: vi.fn().mockName("ExpressionAssignmentService.assignFromGallery")
        };
        personalityService.listExpressions.mockReturnValue(of([{ expression_key: 'happy' } as any]));
        assignmentService.assignFromGallery.mockReturnValue(of({ expression_key: 'happy' } as any));

        await TestBed.configureTestingModule({
            imports: [AssignAsExpressionFlowComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: PersonalityService, useValue: personalityService },
                { provide: ExpressionAssignmentService, useValue: assignmentService },
            ],
        }).compileComponents();
    });

    it('loads expression keys when selecting a personality', () => {
        const fixture = TestBed.createComponent(AssignAsExpressionFlowComponent);
        const component = fixture.componentInstance;
        component.onPersonalityChange('pers-1');
        expect(component.availableExpressionKeys()).toEqual(['happy']);
    });

    function setup() {
        const fixture = TestBed.createComponent(AssignAsExpressionFlowComponent);
        fixture.componentRef.setInput('imageId', 'img-1');
        fixture.componentRef.setInput('imageUrl', '/img-1.png');
        const component = fixture.componentInstance;
        component.selectedPersonalityId.set('pers-1');
        return component;
    }

    it('submits the entered label with the assignment', () => {
        const component = setup();
        component.expressionKey.set(' Happy ');
        component.label.set('  Used when delighted  ');
        component.submit();
        expect(assignmentService.assignFromGallery).toHaveBeenCalledWith(
            'pers-1', 'happy', 'img-1', '/img-1.png', undefined, 'Used when delighted',
        );
    });

    it('submits the prefilled title-case label after choosing a key', () => {
        const component = setup();
        component.chooseKey('very_happy');
        component.submit();
        expect(assignmentService.assignFromGallery).toHaveBeenCalledWith(
            'pers-1', 'very_happy', 'img-1', '/img-1.png', undefined, 'Very Happy',
        );
    });

    it('prefills and keeps the existing label when choosing an existing slot', () => {
        personalityService.listExpressions.mockReturnValue(
            of([{ expression_key: 'happy', label: 'When delighted' } as any]),
        );
        const component = setup();
        component.onPersonalityChange('pers-1');
        component.chooseKey('happy');
        expect(component.label()).toBe('When delighted');
        component.submit();
        expect(assignmentService.assignFromGallery.mock.calls[0][5]).toBe('When delighted');
    });

    it('omits the label when it is blank so an existing label is left unchanged', () => {
        const component = setup();
        component.expressionKey.set('happy');
        component.label.set('   ');
        component.submit();
        expect(assignmentService.assignFromGallery).toHaveBeenCalledWith(
            'pers-1', 'happy', 'img-1', '/img-1.png', undefined, undefined,
        );
    });
});

import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ExpressionAssignmentService } from '../../../core/services/expression-assignment.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { AssignAsExpressionFlowComponent } from './assign-as-expression-flow.component';

describe('AssignAsExpressionFlowComponent', () => {
    beforeEach(async () => {
        const personalityService = {
            listExpressions: vi.fn().mockName("PersonalityService.listExpressions")
        };
        const assignmentService = {
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
});

import type { MockedObject } from "vitest";
import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';

import { PersonalityGettingStartedComponent } from './personality-getting-started.component';

describe('PersonalityGettingStartedComponent', () => {
    let fixture: ComponentFixture<PersonalityGettingStartedComponent>;
    let router: Pick<MockedObject<Router>, 'navigate'>;

    beforeEach(async () => {
        router = {
            navigate: vi.fn().mockName("Router.navigate")
        } as unknown as Pick<MockedObject<Router>, 'navigate'>;
        await TestBed.configureTestingModule({
            imports: [PersonalityGettingStartedComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: Router, useValue: router },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(PersonalityGettingStartedComponent);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders both getting-started choices and navigates from them', () => {
        fixture.detectChanges();
        const buttons = fixture.nativeElement.querySelectorAll('button') as NodeListOf<HTMLButtonElement>;

        expect(buttons.length).toBe(2);
        expect(fixture.nativeElement.textContent).toContain('Generate for Me');
        expect(fixture.nativeElement.textContent).toContain('Create Manually');

        buttons[0].click();
        buttons[1].click();
        expect(router.navigate).toHaveBeenCalledWith(['/personality/generate']);
        expect(router.navigate).toHaveBeenCalledWith(['/personality'], { queryParams: { create: '1' } });
    });
});

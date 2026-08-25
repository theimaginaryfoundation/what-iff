import { Component, signal, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TooltipDirective } from './tooltip.directive';

@Component({
    standalone: true,
    imports: [TooltipDirective],
    changeDetection: ChangeDetectionStrategy.Eager,
    template: '<button uiTooltip="Helpful text">Trigger</button>',
})
class TooltipHostComponent {
}

@Component({
    standalone: true,
    imports: [TooltipDirective],
    changeDetection: ChangeDetectionStrategy.Eager,
    template: '@if (showButton()) { <button uiTooltip="Destroy me">Trigger</button> }',
})
class DestroyableTooltipHostComponent {
    readonly showButton = signal(true);
}

describe('TooltipDirective', () => {
    let fixture: ComponentFixture<TooltipHostComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [TooltipHostComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(TooltipHostComponent);
        fixture.detectChanges();
    });

    it('shows a tooltip on focus and links aria-describedby', () => {
        const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;

        button.dispatchEvent(new Event('focus'));
        fixture.detectChanges();

        const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement;
        expect(tooltip.textContent).toContain('Helpful text');
        expect(button.getAttribute('aria-describedby')).toBe(tooltip.id);
    });

    it('closes on escape', () => {
        const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;

        button.dispatchEvent(new Event('focus'));
        const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement;
        button.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
        fixture.detectChanges();

        expect(tooltip.hasAttribute('hidden')).toBe(true);
        expect(button.hasAttribute('aria-describedby')).toBe(false);
    });

    it('reuses the tooltip element between show and hide cycles', () => {
        const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;

        button.dispatchEvent(new Event('focus'));
        fixture.detectChanges();
        const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement;

        button.dispatchEvent(new Event('blur'));
        button.dispatchEvent(new Event('focus'));
        fixture.detectChanges();

        expect(document.body.querySelector('[role="tooltip"]')).toBe(tooltip);
        expect(tooltip.hasAttribute('hidden')).toBe(false);
    });

    it('removes the tooltip node when the host is destroyed while visible', () => {
        const destroyableFixture = TestBed.createComponent(DestroyableTooltipHostComponent);
        destroyableFixture.detectChanges();
        const button = destroyableFixture.nativeElement.querySelector('button') as HTMLButtonElement;

        button.dispatchEvent(new Event('focus'));
        destroyableFixture.detectChanges();

        expect(document.body.querySelector('[role="tooltip"]')).toBeTruthy();

        destroyableFixture.componentInstance.showButton.set(false);
        destroyableFixture.detectChanges();

        expect(document.body.querySelector('[role="tooltip"]')).toBeNull();
        expect(button.hasAttribute('aria-describedby')).toBe(false);
    });
});

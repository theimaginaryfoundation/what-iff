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

@Component({
    standalone: true,
    imports: [TooltipDirective],
    changeDetection: ChangeDetectionStrategy.Eager,
    template: '<button uiTooltip="A very long thread name" truncatedOnly [truncationTarget]="name"><span #name>A very long thread name</span></button>',
})
class TruncatedTooltipHostComponent {
}

describe('TooltipDirective truncatedOnly', () => {
    const focusAndFindTooltip = (overflowing: boolean): HTMLElement | null => {
        const fixture = TestBed.createComponent(TruncatedTooltipHostComponent);
        fixture.detectChanges();
        const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
        const name = button.querySelector('span') as HTMLElement;
        vi.spyOn(name, 'clientWidth', 'get').mockReturnValue(100);
        vi.spyOn(name, 'scrollWidth', 'get').mockReturnValue(overflowing ? 240 : 100);
        button.dispatchEvent(new Event('focus'));
        const tooltip = document.body.querySelector(`#${button.getAttribute('aria-describedby')}`) as HTMLElement | null;
        fixture.destroy();
        return tooltip;
    };

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [TruncatedTooltipHostComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();
    });

    afterEach(() => vi.restoreAllMocks());

    it('shows the full text when the target is cut off', () => {
        expect(focusAndFindTooltip(true)?.textContent).toBe('A very long thread name');
    });

    it('stays hidden when the text fits', () => {
        expect(focusAndFindTooltip(false)).toBeNull();
    });
});

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

    describe('viewport clamping', () => {
        const showAt = (hostLeft: number, hostWidth: number, tooltipWidth: number): number => {
            const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
            vi.spyOn(button, 'getBoundingClientRect').mockReturnValue(
                { left: hostLeft, right: hostLeft + hostWidth, width: hostWidth, top: 500, bottom: 520, height: 20, x: hostLeft, y: 500, toJSON: () => ({}) } as DOMRect,
            );
            vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(tooltipWidth);
            button.dispatchEvent(new Event('focus'));
            const tooltip = document.body.querySelector('[role="tooltip"]') as HTMLElement;
            return parseFloat(tooltip.style.left);
        };

        beforeEach(() => vi.spyOn(window, 'innerWidth', 'get').mockReturnValue(400));
        afterEach(() => vi.restoreAllMocks());

        it('centres on the host when there is room', () => {
            expect(showAt(150, 100, 120)).toBe(200);
        });

        it('keeps a tooltip near the left edge fully on screen', () => {
            // Host centre is at 30px; a 200px tooltip centred there would start at -70px.
            expect(showAt(10, 40, 200)).toBe(8 + 100);
        });

        it('keeps a tooltip near the right edge fully on screen', () => {
            expect(showAt(360, 30, 200)).toBe(400 - 8 - 100);
        });
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

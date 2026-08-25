import { Component, signal, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { ModalComponent } from './modal.component';
import { resetBodyScrollLockForTests } from '../helpers/body-scroll-lock.helpers';

@Component({
    standalone: true,
    imports: [ModalComponent],
    changeDetection: ChangeDetectionStrategy.Eager,
    template: '<button id="before">Before</button><ui-modal [open]="open()" labelledBy="title" (dismiss)="onDismiss($event)"><h2 modal-header id="title">Title</h2><button>Inside</button><div modal-footer>Footer</div></ui-modal>',
})
class ModalHostComponent {
    open = signal(true);
    dismissed = 0;
    lastReason: string | null = null;
    onDismiss(reason: string): void {
        this.dismissed += 1;
        this.lastReason = reason;
    }
}

describe('ModalComponent', () => {
    let fixture: ComponentFixture<ModalHostComponent>;

    beforeEach(async () => {
        resetBodyScrollLockForTests();
        document.body.classList.remove('overflow-hidden');
        await TestBed.configureTestingModule({ imports: [ModalHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
        fixture = TestBed.createComponent(ModalHostComponent);
        fixture.detectChanges();
        await new Promise(resolve => setTimeout(resolve, 0));
    });

    afterEach(() => {
        document.body.classList.remove('overflow-hidden');
        resetBodyScrollLockForTests();
    });

    it('renders a dialog, locks body scroll, and dismisses on escape', () => {
        const dialog = fixture.nativeElement.querySelector('[role="dialog"]') as HTMLElement;
        expect(dialog.getAttribute('aria-modal')).toBe('true');
        expect(document.body.classList.contains('overflow-hidden')).toBe(true);

        dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

        expect(fixture.componentInstance.dismissed).toBe(1);
        expect(fixture.componentInstance.lastReason).toBe('escape');
    });

    it('reports the dismiss reason for backdrop and close button', () => {
        const backdrop = fixture.nativeElement.querySelector('.ui-modal') as HTMLElement;
        backdrop.click();
        expect(fixture.componentInstance.lastReason).toBe('backdrop');

        const closeButton = fixture.nativeElement.querySelector('.ui-modal__close') as HTMLButtonElement;
        closeButton.click();
        expect(fixture.componentInstance.lastReason).toBe('close-button');
    });

    it('releases body scroll when closed', () => {
        fixture.componentInstance.open.set(false);
        fixture.detectChanges();

        expect(document.body.classList.contains('overflow-hidden')).toBe(false);
    });
});

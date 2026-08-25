import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { ConfirmationModalComponent } from './confirmation-modal.component';
import { ConfirmationService } from '../../services/confirmation.service';

describe('ConfirmationModalComponent', () => {
    let fixture: ComponentFixture<ConfirmationModalComponent>;
    let service: ConfirmationService;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [ConfirmationModalComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(ConfirmationModalComponent);
        service = TestBed.inject(ConfirmationService);
    });

    afterEach(() => {
        service.close();
        document.body.classList.remove('overflow-hidden');
    });

    it('renders through the shared modal primitive and delegates dismiss', async () => {
        vi.spyOn(service, 'onCancel');

        void service.confirm({ title: 'Delete', message: 'Really delete?', type: 'danger' });
        fixture.detectChanges();
        await new Promise(resolve => setTimeout(resolve, 0));

        const dialog = fixture.nativeElement.querySelector('[role="dialog"]') as HTMLElement;
        expect(dialog).toBeTruthy();
        expect(dialog.textContent).toContain('Really delete?');

        dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

        expect(service.onCancel).toHaveBeenCalled();
    });
});

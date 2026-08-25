import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { AddHotkeyModalComponent } from './add-hotkey-modal.component';

describe('AddHotkeyModalComponent', () => {
    let component: AddHotkeyModalComponent;
    let fixture: ComponentFixture<AddHotkeyModalComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [AddHotkeyModalComponent],
            providers: [provideZonelessChangeDetection()]
        }).compileComponents();

        fixture = TestBed.createComponent(AddHotkeyModalComponent);
        component = fixture.componentInstance;
    });

    it('should create', () => {
        expect(component).toBeTruthy();
    });

    describe('outputs', () => {
        it('should emit close when onClose is called', () => {
            vi.spyOn(component.close, 'emit').mockReturnValue(undefined);

            component.onClose();

            expect(component.close.emit).toHaveBeenCalled();
        });

        it('should emit save when onSave is called', () => {
            vi.spyOn(component.save, 'emit').mockReturnValue(undefined);

            component.onSave();

            expect(component.save.emit).toHaveBeenCalled();
        });

        it('should emit bindingValueChange when onValueChange is called', () => {
            vi.spyOn(component.bindingValueChange, 'emit').mockReturnValue(undefined);

            component.onValueChange('ctrl+shift+r');

            expect(component.bindingValueChange.emit).toHaveBeenCalledWith('ctrl+shift+r');
        });
    });

    describe('Escape key', () => {
        it('should call onClose when Escape key is pressed', () => {
            fixture.detectChanges();
            vi.spyOn(component, 'onClose').mockReturnValue(undefined);

            const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true });
            document.dispatchEvent(event);

            expect(component.onClose).toHaveBeenCalled();
        });
    });

    describe('inputs', () => {
        it('should accept bindingValue input', () => {
            component.bindingValue = 'ctrl+1';
            fixture.detectChanges();

            expect(component.bindingValue).toBe('ctrl+1');
        });

        it('should accept bindingError input', () => {
            component.bindingError = 'Hotkey already in use';
            fixture.detectChanges();

            expect(component.bindingError).toBe('Hotkey already in use');
        });

        it('should accept isSaving input', () => {
            component.isSaving = true;
            fixture.detectChanges();

            expect(component.isSaving).toBe(true);
        });
    });
});

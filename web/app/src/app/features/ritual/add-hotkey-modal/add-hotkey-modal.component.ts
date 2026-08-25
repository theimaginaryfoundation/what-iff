import { Component, Input, Output, EventEmitter, ElementRef, AfterViewInit, HostListener, ChangeDetectionStrategy } from '@angular/core';

import { FormsModule } from '@angular/forms';
import { HotkeyInputComponent } from '../../../core/components/hotkey-input/hotkey-input.component';

@Component({
  selector: 'app-add-hotkey-modal',
  standalone: true,
  imports: [FormsModule, HotkeyInputComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  templateUrl: './add-hotkey-modal.component.html'
})
export class AddHotkeyModalComponent {
  @Input() bindingValue: string | null = '';
  @Input() bindingError: string | null = null;
  @Input() isSaving = false;

  @Output() close = new EventEmitter<void>();
  @Output() save = new EventEmitter<void>();
  @Output() bindingValueChange = new EventEmitter<string>();
  constructor(private el: ElementRef) {}

  ngAfterViewInit(): void {
    // Autofocus the first focusable element inside the modal (hotkey input)
    setTimeout(() => {
      const host = this.el.nativeElement as HTMLElement;
      const input = host.querySelector('input, textarea, [contenteditable="true"]') as HTMLElement | null;
      if (input && typeof input.focus === 'function') {
        input.focus();
      }
    }, 0);
  }

  @HostListener('document:keydown', ['$event'])
  handleKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape') {
      this.onClose();
    }
  }

  onClose(): void {
    this.close.emit();
  }

  onSave(): void {
    this.save.emit();
  }

  onValueChange(value: string): void {
    this.bindingValueChange.emit(value);
  }
}


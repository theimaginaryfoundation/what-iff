import { CommonModule } from '@angular/common';
import { Component, input, output, signal, ChangeDetectionStrategy } from '@angular/core';

import { ToolCall } from '../../../../core/models/toolcall.model';

interface ParsedField {
  isJson: boolean;
  value?: string;
  jsonPairs?: Array<{ key: string; value: string }>;
}

@Component({
  selector: 'app-tool-call-detail-modal',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './tool-call-detail-modal.component.html',
  changeDetection: ChangeDetectionStrategy.Eager,
  styleUrls: ['./tool-call-detail-modal.component.scss'],
})
export class ToolCallDetailModalComponent {
  readonly toolCall = input.required<ToolCall>();
  readonly isOpen = input.required<boolean>();
  readonly closeModal = output<void>();

  readonly isInputExpanded = signal(true);
  readonly isOutputExpanded = signal(true);
  readonly isErrorExpanded = signal(true);

  toggleInputSection(): void {
    this.isInputExpanded.set(!this.isInputExpanded());
  }

  toggleOutputSection(): void {
    this.isOutputExpanded.set(!this.isOutputExpanded());
  }

  toggleErrorSection(): void {
    this.isErrorExpanded.set(!this.isErrorExpanded());
  }

  parseField(fieldValue: string): ParsedField {
    if (!fieldValue?.trim()) return { isJson: false, value: '' };

    try {
      const parsed = JSON.parse(fieldValue);
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return {
          isJson: true,
          jsonPairs: Object.entries(parsed).map(([key, value]) => ({
            key,
            value: typeof value === 'object' ? JSON.stringify(value, null, 2) : String(value),
          })),
        };
      }
      return { isJson: false, value: JSON.stringify(parsed, null, 2) };
    } catch {
      return { isJson: false, value: fieldValue };
    }
  }

  get parsedInput(): ParsedField {
    return this.parseField(this.toolCall().tool_input);
  }

  get parsedOutput(): ParsedField {
    return this.parseField(this.toolCall().tool_output);
  }

  get parsedError(): ParsedField {
    const errorValue = this.toolCall().tool_error;
    return errorValue?.trim() ? this.parseField(errorValue) : { isJson: false, value: 'None' };
  }

  formatDateTime(dateString: string): string {
    return new Date(dateString).toLocaleString('en-US', {
      month: 'short',
      day: 'numeric',
      year: 'numeric',
      hour: 'numeric',
      minute: '2-digit',
      second: '2-digit',
      hour12: true,
    });
  }

  onBackdropClick(): void {
    this.closeModal.emit();
  }

  onModalContentClick(event: Event): void {
    event.stopPropagation();
  }
}

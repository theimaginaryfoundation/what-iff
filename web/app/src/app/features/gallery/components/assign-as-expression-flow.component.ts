import { ChangeDetectionStrategy, Component, effect, inject, input, output, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { ExpressionAssignmentService } from '../../../core/services/expression-assignment.service';
import { PersonalityService } from '../../../core/services/personality.service';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';

export interface ExpressionTargetPersonalityOption {
  id: string;
  name: string;
}

@Component({
  selector: 'app-assign-as-expression-flow',
  standalone: true,
  imports: [FormsModule, ModalComponent],
  templateUrl: './assign-as-expression-flow.component.html',
  styleUrl: './assign-as-expression-flow.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AssignAsExpressionFlowComponent {
  private readonly personalityService = inject(PersonalityService);
  private readonly expressionAssignment = inject(ExpressionAssignmentService);

  readonly open = input(false);
  readonly imageId = input<string | null>(null);
  readonly imageUrl = input<string | null>(null);
  readonly personalities = input<ExpressionTargetPersonalityOption[]>([]);

  readonly close = output<void>();
  readonly assigned = output<void>();

  readonly selectedPersonalityId = signal('');
  readonly expressionKey = signal('');
  readonly label = signal('');
  readonly availableExpressionKeys = signal<string[]>([]);
  readonly loadingKeys = signal(false);
  readonly submitting = signal(false);
  readonly error = signal<string | null>(null);

  constructor() {
    effect(() => {
      if (!this.open()) {
        this.resetState();
      }
    });
  }

  onPersonalityChange(personalityId: string): void {
    this.selectedPersonalityId.set(personalityId);
    this.expressionKey.set('');
    this.label.set('');
    if (!personalityId) {
      this.availableExpressionKeys.set([]);
      return;
    }
    this.loadExpressionKeys(personalityId);
  }

  chooseKey(key: string): void {
    this.expressionKey.set(key);
    this.label.set(this.toTitleCase(key));
  }

  submit(): void {
    const imageId = this.imageId();
    if (!imageId || !this.selectedPersonalityId() || !this.expressionKey().trim()) {
      return;
    }

    this.error.set(null);
    this.submitting.set(true);
    this.expressionAssignment
      .assignFromGallery(
        this.selectedPersonalityId(),
        this.expressionKey().trim().toLowerCase(),
        imageId,
        this.imageUrl(),
      )
      .subscribe({
        next: () => {
          this.submitting.set(false);
          this.assigned.emit();
        },
        error: () => {
          this.submitting.set(false);
          this.error.set('Failed to assign image to expression slot.');
        },
      });
  }

  private loadExpressionKeys(personalityId: string): void {
    this.loadingKeys.set(true);
    this.error.set(null);
    this.personalityService.listExpressions(personalityId).subscribe({
      next: rows => {
        this.loadingKeys.set(false);
        this.availableExpressionKeys.set(rows.map(row => row.expression_key).sort());
      },
      error: () => {
        this.loadingKeys.set(false);
        this.availableExpressionKeys.set([]);
        this.error.set('Failed to load expression slots for that personality.');
      },
    });
  }

  private resetState(): void {
    this.selectedPersonalityId.set('');
    this.expressionKey.set('');
    this.label.set('');
    this.availableExpressionKeys.set([]);
    this.loadingKeys.set(false);
    this.submitting.set(false);
    this.error.set(null);
  }

  private toTitleCase(value: string): string {
    return value
      .split(/[_-]/g)
      .map(part => (part ? part[0].toUpperCase() + part.slice(1) : part))
      .join(' ');
  }
}

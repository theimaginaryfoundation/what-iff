import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

/** What the converter could not carry, above an office editor; dismissable (ADR 0x043 §7). */
@Component({
  selector: 'app-fidelity-notice',
  standalone: true,
  template: `
    @if (losses().length || warnings().length) {
      <aside class="fidelity-notice" role="status">
        <div class="fidelity-notice__text">
          @if (losses().length) {
            <p>
              <strong>Some of the original cannot be edited here:</strong> {{ losses().join('; ') }}. Saving over the original drops it; use
              Save as to keep the original file.
            </p>
          }
          @if (warnings().length) {
            <p class="fidelity-notice__minor">Approximated: {{ warnings().join('; ') }}.</p>
          }
        </div>
        <button type="button" class="fidelity-notice__close" aria-label="Dismiss" (click)="dismiss.emit()">×</button>
      </aside>
    }
  `,
  styles: `
    .fidelity-notice {
      align-items: flex-start;
      background: var(--bg-warning, #fff8e1);
      border-bottom: 1px solid var(--border);
      color: var(--text-primary);
      display: flex;
      font-size: 0.8125rem;
      gap: 0.75rem;
      padding: 0.5rem 1rem;
    }
    .fidelity-notice p {
      margin: 0;
    }
    .fidelity-notice__text {
      flex: 1 1 auto;
    }
    .fidelity-notice__minor {
      color: var(--text-secondary);
      margin-top: 0.25rem;
    }
    .fidelity-notice__close {
      background: none;
      border: none;
      color: var(--text-secondary);
      cursor: pointer;
      font-size: 1.1rem;
      line-height: 1;
      padding: 0 0.25rem;
    }
    .fidelity-notice__close:hover {
      color: var(--text-primary);
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FidelityNoticeComponent {
  readonly losses = input<readonly string[]>([]);
  readonly warnings = input<readonly string[]>([]);
  readonly dismiss = output<void>();
}

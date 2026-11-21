import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  effect,
  inject,
  input,
  model,
  output,
  signal,
  untracked,
} from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Subscription } from 'rxjs';

import { Personality } from '../../../core/models/personality.model';
import { PersonalityService } from '../../../core/services/personality.service';
import { ButtonComponent } from '../../../shared/ui/button/button.component';
import { CheckboxComponent } from '../../../shared/ui/checkbox/checkbox.component';
import { ModalComponent } from '../../../shared/ui/modal/modal.component';
import { DEFAULT_THREAD_NAME } from '../../../core/models/chat.model';
import { PersonaPickerComponent } from './persona-picker.component';
import { ThreadTitleFieldComponent } from './thread-title-field.component';

/**
 * Dialog wrapper around `persona-picker`. Fetches the personality list every
 * time the dialog opens and re-emits the picker's `select` / `cancel` /
 * `create` outputs to the parent.
 *
 * The app layout keeps one instance mounted for the whole session, so the
 * list must not be loaded once and kept: a personality created or imported
 * after the first open would be missing (or a deleted one still offered)
 * until a page reload (#318). Reopening shows the last list straight away and
 * swaps in the fresh one when it arrives; only the first open shows the
 * loading state. A change published while the dialog is open refetches too.
 */
@Component({
  selector: 'app-persona-picker-dialog',
  standalone: true,
  imports: [ButtonComponent, CheckboxComponent, ModalComponent, NgTemplateOutlet, PersonaPickerComponent, ThreadTitleFieldComponent],
  template: `
    <ui-modal [open]="open()" [labelledBy]="labelId" size="lg" (dismiss)="onCancel()">
      <div modal-header>
        <div class="grid gap-1">
          <h2 [id]="labelId" class="text-base font-semibold text-(--color-text-primary)">{{ title() }}</h2>
          @if (subtitle()) {
            <p class="m-0 text-xs text-(--color-text-secondary)">{{ subtitle() }}</p>
          }
        </div>
      </div>
      @if (threadForm()) {
        <!-- Everything sits on the modal body's own 16px edge, the same as the header and footer. -->
        <app-thread-title-field
          [(value)]="threadTitle"
          [placeholder]="defaultThreadName"
          (submitted)="onStart()"
        />
        <section class="mt-4 grid gap-2" aria-labelledby="new-thread-personality-heading">
          <h3 id="new-thread-personality-heading" class="m-0 text-sm font-semibold text-(--color-text-primary)">Choose a Personality</h3>
          <ng-container [ngTemplateOutlet]="pickerTpl" />
        </section>
        <section class="mt-4" aria-label="Privacy">
          <div class="border-t border-(--text-muted) pt-4">
            <label class="flex min-w-0 items-start gap-2 text-sm text-(--color-text-primary)" data-testid="new-thread-sandbox">
              <ui-checkbox [checked]="sandboxed()" (checkedChange)="sandboxed.set($event)" />
              <span>
                <span class="font-medium">Private (sandboxed) thread</span>
                <span class="block text-xs text-(--color-text-secondary)">Can't see your other memories, conversations or files. Can't be switched back on later.</span>
              </span>
            </label>
          </div>
        </section>
      } @else {
        <ng-container [ngTemplateOutlet]="pickerTpl" />
      }
      <!-- The footer's own top border (the existing --border gray) runs the full width of the modal. -->
      <div modal-footer class="w-full">
        @if (threadForm()) {
          <!-- Primary first in the DOM (BTN-07); flex-row-reverse with justify-start puts it at the inline end. -->
          <div class="flex flex-row-reverse items-center justify-start gap-2">
            <ui-button variant="primary" [disabled]="!chosen()" data-testid="new-thread-start" (activate)="onStart()">Start thread</ui-button>
            <ui-button variant="ghost" (activate)="onCancel()">Cancel</ui-button>
          </div>
        }
      </div>
    </ui-modal>

    <ng-template #pickerTpl>
      @if (loading()) {
        <p class="text-sm text-(--color-text-secondary)" role="status">Loading personalities…</p>
      } @else if (errorMessage()) {
        <p class="rounded-md border border-red-500 bg-red-500/10 px-3 py-2 text-sm text-red-700 dark:text-red-300" role="alert">{{ errorMessage() }}</p>
      } @else {
        <persona-picker
          [personalities]="personalities()"
          [ariaLabel]="title()"
          [appearance]="pickerAppearance()"
          [showCancel]="!threadForm()"
          [focusSearchOnOpen]="!threadForm()"
          [selectedId]="chosen()?.id ?? null"
          [compact]="threadForm()"
          (select)="onSelect($event)"
          (cancel)="onCancel()"
          (create)="onCreate()"
        />
      }
    </ng-template>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonaPickerDialogComponent {
  readonly open = input<boolean>(false);
  readonly title = input<string>('Pick a personality');
  readonly subtitle = input<string | null>(null);
  /**
   * The new-thread form: Thread Title (default "New thread"), Choose a Personality, and a footer with
   * the Private checkbox, Cancel and Start thread. A click on a
   * personality only marks it, because the settings below it are still to be set; `select` fires from
   * Start thread. The parent reads the settings through `[(sandboxed)]` and `[(threadTitle)]`.
   * Off, a click on a personality picks it at once (every other caller).
   */
  readonly threadForm = input<boolean>(false);
  readonly sandboxed = model<boolean>(false);
  readonly threadTitle = model<string>('');
  readonly defaultThreadName = DEFAULT_THREAD_NAME;
  /** The personality marked in the form, until Start thread. */
  readonly chosen = signal<Personality | null>(null);
  readonly pickerAppearance = input<'rich' | 'filter-list'>('rich');

  readonly select = output<Personality>();
  readonly cancel = output<void>();
  readonly create = output<void>();

  private readonly personalityService = inject(PersonalityService);
  private readonly destroyRef = inject(DestroyRef);

  readonly labelId = `persona-picker-dialog-${randomId()}`;
  readonly personalities = signal<Personality[]>([]);
  readonly loading = signal(false);
  readonly errorMessage = signal<string | null>(null);

  private hasLoaded = false;
  private inFlight: Subscription | null = null;

  constructor() {
    // Each closed -> open transition refetches; staying open does not.
    effect(() => {
      if (!this.open()) return;
      untracked(() => {
        // A form reopens fresh: no stale pick, title or privacy choice from the last time it was cancelled.
        this.chosen.set(null);
        this.sandboxed.set(false);
        this.threadTitle.set(this.threadForm() ? DEFAULT_THREAD_NAME : '');
        this.load();
      });
    });

    this.personalityService.changes$
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe(() => {
        if (this.open()) this.load();
      });

    this.destroyRef.onDestroy(() => this.inFlight?.unsubscribe());
  }

  refresh(): void {
    this.load();
  }

  onSelect(personality: Personality): void {
    if (this.threadForm()) {
      this.chosen.set(personality);
      return;
    }
    this.select.emit(personality);
  }

  /** Start thread: hands the marked personality to the parent, which also reads the settings. */
  onStart(): void {
    const personality = this.chosen();
    if (personality) this.select.emit(personality);
  }

  onCancel(): void {
    this.cancel.emit();
  }

  onCreate(): void {
    this.create.emit();
  }

  private load(): void {
    this.inFlight?.unsubscribe();
    // Stale-while-revalidate: once a list has been shown, keep it on screen while refetching.
    if (!this.hasLoaded) this.loading.set(true);
    this.errorMessage.set(null);
    this.inFlight = this.personalityService.listPersonalities(1, 100).subscribe({
      next: response => {
        this.personalities.set(response.results ?? []);
        this.loading.set(false);
        this.hasLoaded = true;
      },
      error: err => {
        console.error('Failed to load personalities', err);
        this.loading.set(false);
        // A failed background refresh keeps the list the user can already see.
        if (!this.hasLoaded) this.errorMessage.set('Failed to load personalities. Please try again.');
      },
    });
  }
}

function randomId(): string {
  return Math.random().toString(36).slice(2, 10);
}

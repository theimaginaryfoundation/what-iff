import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal, Injector, afterNextRender, inject, ElementRef } from '@angular/core';
import { DatePipe } from '@angular/common';

import { AgentPrompt, AgentPromptResponse } from '../../../../core/models/job.model';
import { ButtonComponent, CheckboxComponent, IconButtonComponent, XIconComponent } from '../../../../shared/ui';
import { AgentPromptChoiceComponent } from './agent-prompt-choice.component';
import { AgentPromptQuestionsComponent } from './agent-prompt-questions.component';
import {
  FormField,
  formError,
  formFields,
  formResponse,
  initialFormValues,
  linkHost,
  promptHeading,
} from './agent-prompt-card.helpers';

/** A form's enum field of up to this many choices is a stack of radios; a longer one is a dropdown. */
const MAX_RADIO_CHOICES = 6;

/**
 * The question a running turn is waiting on (ADR 0x042), docked above the composer: the agent's
 * ask_user questions (AgentPromptQuestionsComponent) or approval request, or an MCP server's
 * elicitation (a form, or a link to open). Emits the person's answer; the session sends it and
 * hides the card.
 */
@Component({
  selector: 'app-agent-prompt-card',
  standalone: true,
  imports: [AgentPromptChoiceComponent, AgentPromptQuestionsComponent, ButtonComponent, CheckboxComponent, DatePipe, IconButtonComponent, XIconComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './agent-prompt-card.shared.scss',
  template: `
    @let p = prompt();
    <section class="prompt" role="region" [attr.aria-label]="heading()" data-testid="agent-prompt-card">
      <header class="prompt__head">
        <span class="prompt__dot" aria-hidden="true"></span>
        <h2 class="prompt__title">{{ heading() }}</h2>
        <span class="prompt__expiry">Waiting until {{ p.expires_at | date: 'shortTime' }}</span>
        @if (p.payload.kind !== 'approval') {
          <ui-icon-button
            size="xs"
            ariaLabel="Dismiss without answering"
            data-testid="agent-prompt-skip"
            (activate)="emit({ action: p.payload.kind === 'questions' ? 'decline' : 'cancel' })"
          >
            <ui-x-icon [size]="14" />
          </ui-icon-button>
        }
      </header>

      @switch (p.payload.kind) {
        @case ('approval') {
          <div class="prompt__body">
            <p class="prompt__action">{{ p.payload.title }}</p>
            @if (p.payload.message) {
              <p class="prompt__message">{{ p.payload.message }}</p>
            }
            @if (noteOpen()) {
              <textarea
                class="prompt__input"
                rows="2"
                aria-label="Note for the agent"
                placeholder="e.g. go ahead, but skip the archive folder"
                [value]="note()"
                (input)="note.set($any($event.target).value)"
              ></textarea>
            } @else {
              <button type="button" class="prompt__add-note" (click)="openNote()" data-testid="agent-prompt-add-note">Add a note for the agent</button>
            }
          </div>
          <footer class="prompt__footer">
            <ui-button size="xs" variant="ghost" (activate)="emit({ action: 'decline', note: note() })" data-testid="agent-prompt-deny">Deny</ui-button>
            <ui-button size="xs" variant="primary" (activate)="emit({ action: 'accept', note: note() })" data-testid="agent-prompt-approve">Approve</ui-button>
          </footer>
        }

        @case ('questions') {
          <app-agent-prompt-questions [promptId]="p.id" [questions]="p.payload.questions ?? []" (answer)="answer.emit($event)" />
        }

        @case ('form') {
          <div class="prompt__body prompt__body--scroll">
            @if (p.payload.message) {
              <p class="prompt__message">{{ p.payload.message }}</p>
            }
            @for (f of fields(); track f.name) {
              <div class="prompt__field">
                @switch (f.control) {
                  @case ('checkbox') {
                    <label class="prompt__check">
                      <ui-checkbox [checked]="!!values()[f.name]" (checkedChange)="setValue(f.name, $event)" />
                      {{ f.label }}
                    </label>
                  }
                  @case ('select') {
                    @if (f.choices.length <= maxRadioChoices) {
                      <span class="prompt__field-label">{{ f.label }}@if (f.required) {<span aria-hidden="true"> *</span>}</span>
                      <div class="prompt__choices" role="radiogroup" [attr.aria-label]="f.label">
                        @for (c of f.choices; track c.value) {
                          <app-prompt-choice [name]="fieldId(f)" [label]="c.label" [checked]="values()[f.name] === c.value" (picked)="setValue(f.name, c.value)" />
                        }
                        @if (!f.required) {
                          <app-prompt-choice [name]="fieldId(f)" label="No answer" [checked]="!values()[f.name]" (picked)="setValue(f.name, '')" />
                        }
                      </div>
                    } @else {
                      <label class="prompt__field-label" [for]="fieldId(f)">{{ f.label }}@if (f.required) {<span aria-hidden="true"> *</span>}</label>
                      <select class="prompt__input" [id]="fieldId(f)" [value]="values()[f.name] ?? ''" (change)="setValue(f.name, $any($event.target).value)">
                        <option value="">Choose…</option>
                        @for (c of f.choices; track c.value) {
                          <option [value]="c.value" [selected]="values()[f.name] === c.value">{{ c.label }}</option>
                        }
                      </select>
                    }
                  }
                  @case ('multi') {
                    <span class="prompt__field-label">{{ f.label }}@if (f.required) {<span aria-hidden="true"> *</span>}</span>
                    <div class="prompt__choices" role="group" [attr.aria-label]="f.label">
                      @for (c of f.choices; track c.value) {
                        <app-prompt-choice [multiple]="true" [label]="c.label" [checked]="multiHas(f, c.value)" (picked)="toggleMulti(f, c.value)" />
                      }
                    </div>
                  }
                  @default {
                    <label class="prompt__field-label" [for]="fieldId(f)">{{ f.label }}@if (f.required) {<span aria-hidden="true"> *</span>}</label>
                    <input
                      class="prompt__input"
                      [id]="fieldId(f)"
                      [type]="f.control"
                      [attr.min]="f.min ?? null"
                      [attr.max]="f.max ?? null"
                      [attr.step]="f.integer ? 1 : null"
                      [value]="values()[f.name] ?? ''"
                      (input)="setValue(f.name, $any($event.target).value)"
                      (keydown.enter)="submitForm()"
                    />
                  }
                }
                @if (f.description && f.control !== 'checkbox') {
                  <span class="prompt__hint">{{ f.description }}</span>
                }
              </div>
            }
            @if (formProblem()) {
              <p class="prompt__error" role="alert">{{ formProblem() }}</p>
            }
          </div>
          <footer class="prompt__footer">
            <p class="prompt__footer-note">Only what you enter here goes to {{ p.payload.server_name || 'the connector' }}.</p>
            <ui-button size="xs" variant="ghost" (activate)="emit({ action: 'decline' })">Decline</ui-button>
            <ui-button size="xs" variant="primary" (activate)="submitForm()" data-testid="agent-prompt-submit">Send</ui-button>
          </footer>
        }

        @case ('url') {
          <div class="prompt__body">
            @if (p.payload.message) {
              <p class="prompt__message">{{ p.payload.message }}</p>
            }
            @if (host(); as h) {
              <p class="prompt__link"><strong>{{ h }}</strong><span class="prompt__link-url">{{ p.payload.url }}</span></p>
            } @else {
              <p class="prompt__error">The connector sent a link that can't be opened safely.</p>
            }
          </div>
          <footer class="prompt__footer">
            <ui-button size="xs" variant="ghost" (activate)="emit({ action: 'decline' })">Decline</ui-button>
            @if (host(); as h) {
              @if (opened()) {
                <ui-button size="xs" variant="secondary" (activate)="openLink()" data-testid="agent-prompt-open-link">Open again</ui-button>
                <ui-button size="xs" variant="primary" (activate)="emit({ action: 'accept' })" data-testid="agent-prompt-finished">I've finished</ui-button>
              } @else {
                <ui-button size="xs" variant="primary" (activate)="openLink()" data-testid="agent-prompt-open-link">Open {{ h }}</ui-button>
              }
            }
          </footer>
        }
      }
    </section>
  `,
  styles: [`
    /* Lined up with the composer's message box below it (chat-composer: same insets and width). */
    :host { display: block; padding: 0 3.25rem 0 1rem; }
    @media (max-width: 1023px) { :host { padding-right: 1rem; } }
    .prompt {
      background: var(--bg-card);
      border: 1px solid var(--border);
      border-radius: 0.75rem;
      box-shadow: var(--card-shadow);
      color: var(--text-primary);
      margin: 0 auto 0.5rem;
      overflow: hidden;
      width: min(100%, 720px);
    }
    .prompt__head { align-items: center; display: flex; gap: 0.5rem; min-height: 2.25rem; padding: 0.25rem 0.375rem 0.25rem 0.75rem; }
    .prompt__dot {
      animation: prompt-pulse 1.6s ease-in-out infinite;
      background: var(--accent);
      border-radius: 999px;
      flex: none;
      height: 0.4375rem;
      width: 0.4375rem;
    }
    @keyframes prompt-pulse { 50% { opacity: 0.35; } }
    @media (prefers-reduced-motion: reduce) { .prompt__dot { animation: none; } }
    .prompt__title { font-size: 0.8125rem; font-weight: 600; margin: 0; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .prompt__expiry { color: var(--text-muted); flex: none; font-size: 0.75rem; margin-left: auto; padding-right: 0.375rem; }
    .prompt__body { display: grid; gap: 0.5rem; padding: 0.25rem 0.75rem 0.75rem; }
    .prompt__body--scroll { gap: 0.75rem; max-height: min(46vh, 24rem); overflow-y: auto; }
    .prompt__action { font-size: 0.9375rem; font-weight: 500; margin: 0; }
    .prompt__message { color: var(--text-secondary); font-size: 0.875rem; margin: 0; white-space: pre-wrap; }
    .prompt__add-note { background: none; border: 0; color: var(--accent); cursor: pointer; font-size: 0.8125rem; justify-self: start; padding: 0; }
    .prompt__add-note:hover { text-decoration: underline; }
    .prompt__add-note:focus-visible { border-radius: 0.25rem; outline: 2px solid var(--focus-ring); outline-offset: 2px; }
    .prompt__field { display: grid; gap: 0.25rem; }
    .prompt__field-label { color: var(--text-secondary); font-size: 0.8125rem; font-weight: 500; }
    .prompt__choices { display: grid; gap: 0.0625rem; margin-inline: -0.5rem; }
    .prompt__check { align-items: center; display: flex; font-size: 0.875rem; gap: 0.625rem; }
    .prompt__hint { color: var(--text-muted); font-size: 0.75rem; margin: 0; }
    .prompt__error { color: var(--danger); font-size: 0.8125rem; margin: 0; }
    .prompt__link { display: grid; font-size: 0.875rem; gap: 0.125rem; margin: 0; overflow-wrap: anywhere; }
    .prompt__link-url { color: var(--text-muted); font-size: 0.8125rem; }
  `],
})
export class AgentPromptCardComponent {
  readonly prompt = input.required<AgentPrompt>();
  readonly answer = output<AgentPromptResponse>();

  private readonly element = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

  readonly heading = computed(() => promptHeading(this.prompt().payload, this.prompt().source));
  readonly maxRadioChoices = MAX_RADIO_CHOICES;
  readonly note = signal('');
  readonly noteOpen = signal(false);
  readonly fields = computed<FormField[]>(() => formFields(this.prompt().payload.schema));
  readonly values = signal<Record<string, unknown>>({});
  readonly formProblem = signal<string | null>(null);
  readonly host = computed(() => linkHost(this.prompt().payload.url));
  readonly opened = signal(false);

  constructor() {
    // A new prompt starts from a clean card (and the form from its defaults).
    let lastId: string | null = null;
    effect(() => {
      const p = this.prompt();
      if (p.id === lastId) return;
      lastId = p.id;
      this.note.set('');
      this.noteOpen.set(false);
      this.values.set(initialFormValues(formFields(p.payload.schema)));
      this.formProblem.set(null);
      this.opened.set(false);
    });
  }

  openNote(): void {
    this.noteOpen.set(true);
    afterNextRender(() => this.element.nativeElement.querySelector('textarea')?.focus(), { injector: this.injector });
  }

  setValue(name: string, value: unknown): void {
    this.values.update(v => ({ ...v, [name]: value }));
    this.formProblem.set(null);
  }

  multiHas(f: FormField, value: string): boolean {
    const v = this.values()[f.name];
    return Array.isArray(v) && v.includes(value);
  }

  toggleMulti(f: FormField, value: string): void {
    const cur = this.values()[f.name];
    const list = Array.isArray(cur) ? (cur as string[]) : [];
    this.setValue(f.name, list.includes(value) ? list.filter(x => x !== value) : [...list, value]);
  }

  fieldId(f: FormField): string {
    return `prompt-${this.prompt().id}-${f.name}`;
  }

  submitForm(): void {
    const problem = formError(this.fields(), this.values());
    if (problem) {
      this.formProblem.set(problem);
      return;
    }
    this.emit(formResponse(this.fields(), this.values()));
  }

  openLink(): void {
    const url = this.prompt().payload.url;
    if (!url || !this.host()) return;
    window.open(url, '_blank', 'noopener,noreferrer');
    this.opened.set(true);
  }

  emit(response: AgentPromptResponse): void {
    const note = response.note?.trim();
    this.answer.emit(note ? { ...response, note } : { ...response, note: undefined });
  }
}

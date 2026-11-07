import { ChangeDetectionStrategy, Component, computed, effect, input, output, signal } from '@angular/core';
import { DatePipe } from '@angular/common';

import { AgentPrompt, AgentPromptQuestion, AgentPromptResponse } from '../../../../core/models/job.model';
import { ButtonComponent, CheckboxComponent } from '../../../../shared/ui';
import {
  FormField,
  QuestionDraft,
  emptyDrafts,
  formError,
  formFields,
  formResponse,
  initialFormValues,
  linkHost,
  promptHeading,
  questionAnswered,
  questionsResponse,
  setOwnText,
  toggleOption,
} from './agent-prompt-card.helpers';

/**
 * The question a running turn is waiting on (ADR 0x042), docked above the composer: the agent's
 * ask_user questions or approval request, or an MCP server's elicitation (a form, or a link to
 * open). Emits the person's answer; the session sends it and hides the card.
 */
@Component({
  selector: 'app-agent-prompt-card',
  standalone: true,
  imports: [ButtonComponent, CheckboxComponent, DatePipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @let p = prompt();
    <section class="prompt" role="region" [attr.aria-label]="heading()" data-testid="agent-prompt-card">
      <header class="prompt__head">
        <span class="prompt__dot" aria-hidden="true"></span>
        <h2 class="prompt__title">{{ heading() }}</h2>
        <span class="prompt__expiry">Waiting for you · until {{ p.expires_at | date: 'shortTime' }}</span>
      </header>

      @switch (p.payload.kind) {
        @case ('approval') {
          <p class="prompt__action">{{ p.payload.title }}</p>
          @if (p.payload.message) {
            <p class="prompt__message">{{ p.payload.message }}</p>
          }
          <label class="prompt__note-label" for="prompt-note-{{ p.id }}">Note for the agent (optional)</label>
          <textarea
            id="prompt-note-{{ p.id }}"
            class="prompt__input prompt__note"
            rows="2"
            [value]="note()"
            (input)="note.set($any($event.target).value)"
            placeholder="e.g. go ahead, but skip the archive folder"
          ></textarea>
          <div class="prompt__actions">
            <ui-button variant="primary" (activate)="emit({ action: 'accept', note: note() })" data-testid="agent-prompt-approve">Approve</ui-button>
            <ui-button variant="secondary" (activate)="emit({ action: 'decline', note: note() })" data-testid="agent-prompt-deny">Deny</ui-button>
          </div>
        }

        @case ('questions') {
          @for (q of p.payload.questions ?? []; track q.id) {
            @let d = drafts()[q.id];
            <fieldset class="prompt__question">
              <legend class="prompt__legend">
                @if (q.header) {
                  <span class="prompt__chip">{{ q.header }}</span>
                }
                {{ q.question }}
              </legend>
              @if (q.options?.length) {
                <div class="prompt__options" [attr.role]="q.multi_select ? 'group' : 'radiogroup'">
                  @for (o of q.options; track o.label) {
                    <button
                      type="button"
                      class="prompt__option"
                      [class.prompt__option--on]="d?.selected?.includes(o.label)"
                      [attr.role]="q.multi_select ? 'checkbox' : 'radio'"
                      [attr.aria-checked]="d?.selected?.includes(o.label) ?? false"
                      (click)="pick(q, o.label)"
                    >
                      <span class="prompt__option-label">{{ o.label }}</span>
                      @if (o.description) {
                        <span class="prompt__option-desc">{{ o.description }}</span>
                      }
                    </button>
                  }
                </div>
              }
              <input
                class="prompt__input"
                type="text"
                [attr.aria-label]="q.options?.length ? 'Your own answer to: ' + q.question : 'Answer to: ' + q.question"
                [placeholder]="q.options?.length ? 'Or write your own answer…' : 'Your answer…'"
                [value]="d?.text ?? ''"
                (input)="type(q, $any($event.target).value)"
                (keydown.enter)="submitQuestions()"
              />
            </fieldset>
          }
          <div class="prompt__actions">
            <ui-button variant="primary" [disabled]="!anyAnswered()" (activate)="submitQuestions()" data-testid="agent-prompt-submit">Send answer</ui-button>
            <ui-button variant="ghost" (activate)="emit({ action: 'decline' })" data-testid="agent-prompt-skip">Skip</ui-button>
          </div>
        }

        @case ('form') {
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
                  <label class="prompt__field-label" [for]="fieldId(f)">{{ f.label }}@if (f.required) {<span aria-hidden="true"> *</span>}</label>
                  <select class="prompt__input" [id]="fieldId(f)" [value]="values()[f.name] ?? ''" (change)="setValue(f.name, $any($event.target).value)">
                    <option value="">Choose…</option>
                    @for (c of f.choices; track c.value) {
                      <option [value]="c.value" [selected]="values()[f.name] === c.value">{{ c.label }}</option>
                    }
                  </select>
                }
                @case ('multi') {
                  <span class="prompt__field-label">{{ f.label }}</span>
                  @for (c of f.choices; track c.value) {
                    <label class="prompt__check">
                      <ui-checkbox [checked]="multiHas(f, c.value)" (checkedChange)="toggleMulti(f, c.value)" />
                      {{ c.label }}
                    </label>
                  }
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
          <div class="prompt__actions">
            <ui-button variant="primary" (activate)="submitForm()" data-testid="agent-prompt-submit">Send</ui-button>
            <ui-button variant="secondary" (activate)="emit({ action: 'decline' })">Decline</ui-button>
            <ui-button variant="ghost" (activate)="emit({ action: 'cancel' })">Cancel</ui-button>
          </div>
          <p class="prompt__fineprint">Only what you enter here is sent back to {{ p.payload.server_name || 'the connector' }}.</p>
        }

        @case ('url') {
          @if (p.payload.message) {
            <p class="prompt__message">{{ p.payload.message }}</p>
          }
          @if (host(); as h) {
            <p class="prompt__link">Opens <strong>{{ h }}</strong><br /><span class="prompt__link-url">{{ p.payload.url }}</span></p>
            <div class="prompt__actions">
              <ui-button variant="primary" (activate)="openLink()" data-testid="agent-prompt-open-link">Open {{ h }}</ui-button>
              <ui-button variant="secondary" [disabled]="!opened()" (activate)="emit({ action: 'accept' })">I've finished</ui-button>
              <ui-button variant="ghost" (activate)="emit({ action: 'decline' })">Decline</ui-button>
            </div>
          } @else {
            <p class="prompt__error">The connector sent a link that can't be opened safely.</p>
            <div class="prompt__actions">
              <ui-button variant="secondary" (activate)="emit({ action: 'decline' })">Decline</ui-button>
            </div>
          }
        }
      }
    </section>
  `,
  styles: [`
    :host { display: block; }
    .prompt {
      background: var(--bg-card);
      border: 1px solid var(--accent);
      border-radius: 0.75rem;
      box-shadow: var(--card-shadow);
      color: var(--text-primary);
      display: grid;
      gap: 0.625rem;
      margin: 0 auto 0.5rem;
      max-height: min(60vh, 32rem);
      overflow-y: auto;
      padding: 0.875rem 1rem;
      width: min(100%, 48rem);
    }
    .prompt__head { align-items: center; display: flex; flex-wrap: wrap; gap: 0.5rem; }
    .prompt__dot {
      animation: prompt-pulse 1.6s ease-in-out infinite;
      background: var(--accent);
      border-radius: 999px;
      height: 0.5rem;
      width: 0.5rem;
    }
    @keyframes prompt-pulse { 50% { opacity: 0.35; } }
    @media (prefers-reduced-motion: reduce) { .prompt__dot { animation: none; } }
    .prompt__title { font-size: 0.9375rem; font-weight: 600; margin: 0; }
    .prompt__expiry { color: var(--text-muted); font-size: 0.75rem; margin-left: auto; }
    .prompt__action { font-weight: 600; margin: 0; }
    .prompt__message { color: var(--text-secondary); margin: 0; white-space: pre-wrap; }
    .prompt__question { border: 0; display: grid; gap: 0.5rem; margin: 0; min-width: 0; padding: 0; }
    .prompt__legend { font-weight: 500; margin-bottom: 0.25rem; padding: 0; }
    .prompt__chip {
      background: var(--accent-bg);
      border-radius: 999px;
      color: var(--accent);
      font-size: 0.6875rem;
      font-weight: 600;
      margin-right: 0.375rem;
      padding: 0.125rem 0.5rem;
      text-transform: uppercase;
    }
    .prompt__options { display: grid; gap: 0.375rem; grid-template-columns: repeat(auto-fill, minmax(12rem, 1fr)); }
    .prompt__option {
      background: var(--bg-surface);
      border: 1px solid var(--border);
      border-radius: 0.5rem;
      color: var(--text-primary);
      cursor: pointer;
      display: grid;
      gap: 0.125rem;
      padding: 0.5rem 0.75rem;
      text-align: left;
    }
    .prompt__option:hover { border-color: var(--border-hover); }
    .prompt__option:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }
    .prompt__option--on { background: var(--accent-bg); border-color: var(--accent); }
    .prompt__option-label { font-weight: 500; }
    .prompt__option-desc { color: var(--text-muted); font-size: 0.8125rem; }
    .prompt__input {
      background: var(--bg-input);
      border: 1px solid var(--border);
      border-radius: 0.5rem;
      color: var(--text-primary);
      font: inherit;
      padding: 0.5rem 0.625rem;
      width: 100%;
    }
    .prompt__input:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
    .prompt__note { resize: vertical; }
    .prompt__note-label, .prompt__field-label { color: var(--text-secondary); font-size: 0.8125rem; }
    .prompt__field { display: grid; gap: 0.25rem; }
    .prompt__check { align-items: center; display: flex; gap: 0.5rem; }
    .prompt__hint, .prompt__fineprint { color: var(--text-muted); font-size: 0.75rem; margin: 0; }
    .prompt__error { color: var(--danger); font-size: 0.8125rem; margin: 0; }
    .prompt__link { margin: 0; overflow-wrap: anywhere; }
    .prompt__link-url { color: var(--text-muted); font-size: 0.8125rem; }
    .prompt__actions { display: flex; flex-wrap: wrap; gap: 0.5rem; }
  `],
})
export class AgentPromptCardComponent {
  readonly prompt = input.required<AgentPrompt>();
  readonly answer = output<AgentPromptResponse>();

  readonly heading = computed(() => promptHeading(this.prompt().payload, this.prompt().source));
  readonly drafts = signal<Record<string, QuestionDraft>>({});
  readonly note = signal('');
  readonly fields = computed<FormField[]>(() => formFields(this.prompt().payload.schema));
  readonly values = signal<Record<string, unknown>>({});
  readonly formProblem = signal<string | null>(null);
  readonly host = computed(() => linkHost(this.prompt().payload.url));
  readonly opened = signal(false);
  readonly anyAnswered = computed(() => Object.values(this.drafts()).some(questionAnswered));

  constructor() {
    // A new prompt starts from a clean card (and the form from its defaults).
    let lastId: string | null = null;
    effect(() => {
      const p = this.prompt();
      if (p.id === lastId) return;
      lastId = p.id;
      this.drafts.set(emptyDrafts(p.payload.questions));
      this.note.set('');
      this.values.set(initialFormValues(formFields(p.payload.schema)));
      this.formProblem.set(null);
      this.opened.set(false);
    });
  }

  pick(q: AgentPromptQuestion, label: string): void {
    this.drafts.update(all => ({ ...all, [q.id]: toggleOption(all[q.id] ?? { selected: [], text: '' }, q, label) }));
  }

  type(q: AgentPromptQuestion, text: string): void {
    this.drafts.update(all => ({ ...all, [q.id]: setOwnText(all[q.id] ?? { selected: [], text: '' }, q, text) }));
  }

  submitQuestions(): void {
    if (!this.anyAnswered()) return;
    this.emit(questionsResponse(this.drafts()));
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

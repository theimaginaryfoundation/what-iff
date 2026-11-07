import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  computed,
  effect,
  inject,
  Injector,
  input,
  output,
  signal,
  untracked,
} from '@angular/core';

import { NgTemplateOutlet } from '@angular/common';

import { AgentPromptQuestion, AgentPromptResponse } from '../../../../core/models/job.model';
import { ButtonComponent, CheckIconComponent, TabComponent, TabPanelComponent, TabsComponent } from '../../../../shared/ui';
import { AgentPromptChoiceComponent, PromptChoicePick } from './agent-prompt-choice.component';
import {
  QuestionDraft,
  emptyDrafts,
  questionAnswered,
  questionComplete,
  questionsResponse,
  questionTabLabel,
  setOtherChosen,
  setOwnText,
  skipQuestion,
  toggleOption,
} from './agent-prompt-card.helpers';

/** Long enough to see the radio fill before a click moves on to the next question. */
const AUTO_ADVANCE_MS = 180;

/**
 * An `ask_user` prompt's questions, one at a time (ADR 0x042): tabs to move between them, then a
 * send that stays disabled until every required question is answered. Answers are stacked rows; a
 * text box only appears when the person picks "Other" (or chooses to answer an optional open
 * question). A required open question has nothing to pick, so its box is simply there.
 */
@Component({
  selector: 'app-agent-prompt-questions',
  standalone: true,
  imports: [AgentPromptChoiceComponent, ButtonComponent, NgTemplateOutlet, CheckIconComponent, TabComponent, TabPanelComponent, TabsComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styleUrl: './agent-prompt-card.shared.scss',
  template: `
    @if (many()) {
      <ui-tabs size="sm" ariaLabel="Questions" [activeId]="tabId(current())" (activeIdChange)="goToTab($event)">
        @for (q of questions(); track q.id; let i = $index) {
          <ui-tab [id]="tabId(q)" [controls]="panelId(q)">
            @if (isAnswered(q)) {
              <ui-check-icon class="steps__tick" [size]="12" />
              <span class="sr-only">Answered: </span>
            }
            <span class="steps__tab-label">{{ tabLabel(q, i) }}</span>
          </ui-tab>
        }
        <ui-tab-panel [id]="panelId(current())" [labelledBy]="tabId(current())">
          <ng-container [ngTemplateOutlet]="panel" />
        </ui-tab-panel>
      </ui-tabs>
    } @else {
      <ng-container [ngTemplateOutlet]="panel" />
    }

    <ng-template #panel>
      @let q = current();
      @let d = draft();
      <fieldset class="steps__panel" (keydown.enter)="onEnter($event)">
        @if (many()) {
          <p class="steps__count">Question {{ step() + 1 }} of {{ questions().length }}@if (q.optional) { · optional }</p>
        }
        <legend class="sr-only">{{ q.question }}</legend>
        <p class="steps__question" aria-hidden="true">{{ q.question }}</p>

        @if (q.options?.length) {
          <div class="steps__choices" [attr.role]="q.multi_select ? 'group' : 'radiogroup'" [attr.aria-label]="q.question">
            @for (o of q.options; track o.label) {
              <app-prompt-choice
                [name]="groupName(q)"
                [multiple]="!!q.multi_select"
                [label]="o.label"
                [description]="o.description"
                [checked]="d.selected.includes(o.label)"
                (picked)="pick(q, o.label, $event)"
              />
            }
            <app-prompt-choice
              [name]="groupName(q)"
              [multiple]="!!q.multi_select"
              label="Other…"
              [checked]="d.other"
              (picked)="chooseOther(q, $event.checked)"
            />
            @if (q.optional && !q.multi_select) {
              <app-prompt-choice
                [name]="groupName(q)"
                label="Skip this one"
                [checked]="!questionAnswered(d) && !d.other"
                (picked)="skip(q)"
              />
            }
          </div>
        } @else if (q.optional) {
          <div class="steps__choices" role="radiogroup" [attr.aria-label]="q.question">
            <app-prompt-choice [name]="groupName(q)" label="Nothing to add" [checked]="!d.other" (picked)="chooseOther(q, false)" />
            <app-prompt-choice [name]="groupName(q)" label="Add something…" [checked]="d.other" (picked)="chooseOther(q, true)" />
          </div>
        }

        @if (d.other) {
          <textarea
            class="prompt__input steps__text"
            rows="2"
            data-testid="agent-prompt-own-answer"
            [class.steps__text--nested]="q.options?.length || q.optional"
            [attr.aria-label]="'Your answer to: ' + q.question"
            placeholder="Type your answer…"
            [value]="d.text"
            (input)="type(q, $any($event.target).value)"
          ></textarea>
        }
      </fieldset>
    </ng-template>

    <footer class="prompt__footer">
      @if (onLast() && !allComplete()) {
        <p class="prompt__footer-note" data-testid="agent-prompt-remaining">Still to answer: {{ remaining() }}</p>
      }
      @if (many()) {
        <ui-button size="xs" variant="ghost" [disabled]="step() === 0" (activate)="go(step() - 1)" data-testid="agent-prompt-back">Back</ui-button>
      }
      @if (!onLast()) {
        <ui-button size="xs" [variant]="allComplete() ? 'secondary' : 'primary'" (activate)="go(step() + 1)" data-testid="agent-prompt-next">Next</ui-button>
      }
      @if (onLast() || allComplete()) {
        <ui-button size="xs" variant="primary" [disabled]="!allComplete()" (activate)="submit()" data-testid="agent-prompt-submit">{{ many() ? 'Send answers' : 'Send answer' }}</ui-button>
      }
    </footer>
  `,
  styles: [`
    :host { display: block; }
    ui-tabs { padding: 0.125rem 0.75rem 0; }
    .steps__tick { color: var(--success); display: inline-flex; }
    .steps__tab-label { display: inline-block; max-width: 9rem; overflow: hidden; text-overflow: ellipsis; }
    .steps__panel {
      border: 0;
      display: grid;
      gap: 0.5rem;
      margin: 0;
      max-height: min(42vh, 22rem);
      min-width: 0;
      overflow-y: auto;
      padding: 0.75rem 0.25rem 0.625rem;
    }
    :host(:not(.steps--many)) .steps__panel { padding-top: 0.25rem; }
    ui-tabs .steps__panel { margin: 0 -0.75rem; padding-inline: 0.25rem; }
    .steps__count { color: var(--text-muted); font-size: 0.75rem; margin: 0; padding-inline: 0.5rem; }
    .steps__question { color: var(--text-primary); font-size: 0.9375rem; font-weight: 500; line-height: 1.375rem; margin: 0; padding-inline: 0.5rem; white-space: pre-wrap; }
    .steps__choices { display: grid; gap: 0.0625rem; }
    .steps__text { justify-self: stretch; margin-inline: 0.5rem; width: calc(100% - 1rem); }
    .steps__text--nested { margin-left: 2.125rem; width: calc(100% - 2.625rem); }
    .sr-only { border: 0; clip: rect(0 0 0 0); height: 1px; margin: -1px; overflow: hidden; padding: 0; position: absolute; white-space: nowrap; width: 1px; }
  `],
  host: { '[class.steps--many]': 'many()' },
})
export class AgentPromptQuestionsComponent {
  readonly promptId = input.required<string>();
  readonly questions = input.required<readonly AgentPromptQuestion[]>();
  readonly answer = output<AgentPromptResponse>();

  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly injector = inject(Injector);

  readonly step = signal(0);
  readonly drafts = signal<Record<string, QuestionDraft>>({});

  readonly many = computed(() => this.questions().length > 1);
  readonly current = computed(() => this.questions()[Math.min(this.step(), this.questions().length - 1)]);
  readonly draft = computed<QuestionDraft>(() => this.drafts()[this.current().id] ?? { selected: [], text: '', other: false });
  readonly onLast = computed(() => this.step() >= this.questions().length - 1);
  readonly unanswered = computed(() => this.questions().filter(q => !questionComplete(q, this.drafts()[q.id])));
  readonly allComplete = computed(() => this.unanswered().length === 0);
  readonly remaining = computed(() => this.unanswered().map(q => questionTabLabel(q, this.questions().indexOf(q))).join(', '));

  protected readonly questionAnswered = questionAnswered;
  private advanceTimer: ReturnType<typeof setTimeout> | null = null;

  constructor() {
    // A new prompt starts clean, on its first question. Only the id counts: the job poll hands in
    // a fresh copy of the same prompt every second.
    effect(() => {
      this.promptId();
      untracked(() => {
        this.drafts.set(emptyDrafts(this.questions()));
        this.step.set(0);
        this.cancelAdvance();
      });
    });
    inject(DestroyRef).onDestroy(() => this.cancelAdvance());
  }

  tabId(q: AgentPromptQuestion): string {
    return `prompt-${this.promptId()}-tab-${q.id}`;
  }

  panelId(q: AgentPromptQuestion): string {
    return `prompt-${this.promptId()}-panel-${q.id}`;
  }

  groupName(q: AgentPromptQuestion): string {
    return `prompt-${this.promptId()}-${q.id}`;
  }

  tabLabel(q: AgentPromptQuestion, index: number): string {
    return questionTabLabel(q, index);
  }

  isAnswered(q: AgentPromptQuestion): boolean {
    return questionAnswered(this.drafts()[q.id]);
  }

  goToTab(tabId: string): void {
    const index = this.questions().findIndex(q => this.tabId(q) === tabId);
    if (index >= 0) this.go(index);
  }

  go(index: number, focusAnswer = false): void {
    this.cancelAdvance();
    const last = this.questions().length - 1;
    this.step.set(Math.max(0, Math.min(index, last)));
    // A person typing their way through keeps their place; a pointer leaves focus alone, except to
    // make an open question ready to type in where there is a real keyboard.
    afterNextRender(() => this.focusStep(focusAnswer), { injector: this.injector });
  }

  /** Picks an option. A click on a single-select question's first answer moves on by itself. */
  pick(q: AgentPromptQuestion, label: string, how: PromptChoicePick): void {
    const before = this.drafts()[q.id];
    const firstAnswer = !questionAnswered(before);
    this.update(q, d => toggleOption(d, q, label));
    if (how.byPointer && firstAnswer && !q.multi_select && !this.onLast()) {
      this.cancelAdvance();
      this.advanceTimer = setTimeout(() => this.go(this.step() + 1), AUTO_ADVANCE_MS);
    }
  }

  /** Turns the question's own-answer row on or off; turning it on brings up the box, ready to type. */
  chooseOther(q: AgentPromptQuestion, chosen: boolean): void {
    this.cancelAdvance();
    this.update(q, d => setOtherChosen(d, q, chosen));
    if (chosen) afterNextRender(() => this.textBox()?.focus(), { injector: this.injector });
  }

  skip(q: AgentPromptQuestion): void {
    this.cancelAdvance();
    this.update(q, d => skipQuestion(d));
  }

  type(q: AgentPromptQuestion, text: string): void {
    this.update(q, d => setOwnText(d, text));
  }

  /** Enter moves on (Shift+Enter is a new line in a text box), and sends from the last question once all is answered. */
  onEnter(event: Event): void {
    const key = event as KeyboardEvent;
    if (key.isComposing || (key.shiftKey && (key.target as HTMLElement).tagName === 'TEXTAREA')) return;
    key.preventDefault();
    if (this.onLast()) {
      this.submit();
    } else {
      this.go(this.step() + 1, true);
    }
  }

  submit(): void {
    if (this.allComplete()) this.answer.emit(questionsResponse(this.drafts()));
  }

  private update(q: AgentPromptQuestion, change: (draft: QuestionDraft) => QuestionDraft): void {
    this.drafts.update(all => ({ ...all, [q.id]: change(all[q.id] ?? { selected: [], text: '', other: false }) }));
  }

  private textBox(): HTMLTextAreaElement | null {
    return this.host.nativeElement.querySelector('textarea');
  }

  private focusStep(focusAnswer: boolean): void {
    const root = this.host.nativeElement;
    const box = this.textBox();
    if (box && (focusAnswer || hasFinePointer(root))) {
      box.focus();
      return;
    }
    if (focusAnswer) root.querySelector<HTMLInputElement>('fieldset input:checked, fieldset input')?.focus();
  }

  private cancelAdvance(): void {
    if (this.advanceTimer !== null) clearTimeout(this.advanceTimer);
    this.advanceTimer = null;
  }
}

function hasFinePointer(el: HTMLElement): boolean {
  return el.ownerDocument.defaultView?.matchMedia?.('(pointer: fine)').matches ?? false;
}

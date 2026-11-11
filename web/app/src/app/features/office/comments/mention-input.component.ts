import { ChangeDetectionStrategy, Component, ElementRef, afterNextRender, computed, input, output, signal, viewChild } from '@angular/core';

import { ButtonComponent } from '../../../shared/ui';
import { CommentDraft, CommentMention, CommentPerson, keptMentions } from './comment-threads';

const MAX_SUGGESTIONS = 6;
/** An `@` at the start or after a space, then what has been typed of a name so far. */
const MENTION_QUERY = /(?:^|\s)@([^\s@]{0,40})$/;

let nextListId = 0;

/**
 * The box a comment is written in (ADR 0x045): a text area where typing `@` offers the people the
 * comment can mention. Picking one writes `@Name` into the text and records who it is, so a
 * mention keeps pointing at the person even when two share a first name. Enter posts, Shift+Enter
 * starts a new line, Escape closes the list or cancels.
 */
@Component({
  selector: 'app-mention-input',
  standalone: true,
  imports: [ButtonComponent],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="mention-input">
      <textarea
        #box
        class="mention-input__box"
        rows="2"
        [attr.aria-label]="label()"
        [attr.placeholder]="placeholder()"
        role="combobox"
        aria-autocomplete="list"
        [attr.aria-expanded]="suggestions().length > 0"
        [attr.aria-controls]="listId"
        [attr.aria-activedescendant]="suggestions().length ? listId + '-' + highlighted() : null"
        [value]="text()"
        (input)="onInput()"
        (keydown)="onKeydown($event)"
        (click)="onInput()"
        (blur)="closeSoon()"
      ></textarea>
      @if (suggestions().length) {
        <ul class="mention-input__list" role="listbox" [id]="listId" aria-label="People to mention">
          @for (person of suggestions(); track person.id; let i = $index) {
            <li
              class="mention-input__option"
              role="option"
              [id]="listId + '-' + i"
              [class.mention-input__option--active]="i === highlighted()"
              [attr.aria-selected]="i === highlighted()"
              (mousedown)="$event.preventDefault(); pick(person)"
            >
              {{ person.name }}
            </li>
          }
        </ul>
      }
      <div class="mention-input__actions">
        @if (cancellable()) {
          <ui-button variant="ghost" size="xs" (activate)="cancel.emit()">Cancel</ui-button>
        }
        <ui-button variant="primary" size="xs" [disabled]="!text().trim()" (activate)="post()">{{ submitLabel() }}</ui-button>
      </div>
    </div>
  `,
  styleUrl: './mention-input.component.scss',
})
export class MentionInputComponent {
  readonly people = input<readonly CommentPerson[]>([]);
  readonly label = input('Comment');
  readonly placeholder = input('Comment, or @mention someone');
  readonly submitLabel = input('Comment');
  readonly cancellable = input(true);
  /** Text and mentions to start from, when editing a comment. */
  readonly initial = input<CommentDraft | null>(null);
  readonly autofocus = input(false);

  readonly submitted = output<CommentDraft>();
  readonly cancel = output<void>();

  protected readonly listId = `mention-list-${++nextListId}`;
  private readonly box = viewChild.required<ElementRef<HTMLTextAreaElement>>('box');
  protected readonly text = signal('');
  private readonly mentions = signal<CommentMention[]>([]);
  /** What has been typed after an `@`, or null when no name is being typed. */
  private readonly query = signal<string | null>(null);
  protected readonly highlighted = signal(0);

  protected readonly suggestions = computed<readonly CommentPerson[]>(() => {
    const query = this.query();
    if (query === null) return [];
    const q = query.toLowerCase();
    const starts = this.people().filter(p => p.name.toLowerCase().startsWith(q));
    const contains = this.people().filter(p => !starts.includes(p) && p.name.toLowerCase().includes(q));
    return [...starts, ...contains].slice(0, MAX_SUGGESTIONS);
  });

  constructor() {
    afterNextRender(() => {
      const initial = this.initial();
      if (initial) {
        this.text.set(initial.text);
        this.mentions.set([...initial.mentions]);
        this.box().nativeElement.value = initial.text;
      }
      if (this.autofocus()) this.focus();
    });
  }

  focus(): void {
    const el = this.box().nativeElement;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }

  /** Clears the box after a post. */
  reset(): void {
    this.text.set('');
    this.mentions.set([]);
    this.query.set(null);
    this.box().nativeElement.value = '';
  }

  protected onInput(): void {
    const el = this.box().nativeElement;
    this.text.set(el.value);
    const before = el.value.slice(0, el.selectionStart ?? el.value.length);
    const match = MENTION_QUERY.exec(before);
    this.query.set(match ? match[1] : null);
    this.highlighted.set(0);
  }

  protected onKeydown(event: KeyboardEvent): void {
    const open = this.suggestions().length > 0;
    if (open && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault();
      const n = this.suggestions().length;
      this.highlighted.update(i => (i + (event.key === 'ArrowDown' ? 1 : n - 1)) % n);
      return;
    }
    if (open && (event.key === 'Enter' || event.key === 'Tab')) {
      event.preventDefault();
      this.pick(this.suggestions()[this.highlighted()]);
      return;
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      if (open) this.query.set(null);
      else if (this.cancellable()) this.cancel.emit();
      return;
    }
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      this.post();
    }
  }

  protected pick(person: CommentPerson): void {
    const el = this.box().nativeElement;
    const caret = el.selectionStart ?? el.value.length;
    const before = el.value.slice(0, caret);
    const match = MENTION_QUERY.exec(before);
    if (!match) return;
    const start = caret - match[1].length - 1;
    const inserted = `@${person.name} `;
    const value = el.value.slice(0, start) + inserted + el.value.slice(caret);
    el.value = value;
    el.setSelectionRange(start + inserted.length, start + inserted.length);
    this.text.set(value);
    this.mentions.update(list => [...list, { id: person.id, name: person.name }]);
    this.query.set(null);
    el.focus();
  }

  protected closeSoon(): void {
    // A click on a suggestion lands before the blur is acted on.
    setTimeout(() => this.query.set(null), 150);
  }

  protected post(): void {
    const text = this.text().trim();
    if (!text) return;
    this.submitted.emit({ text, mentions: keptMentions(text, this.mentions()) });
  }
}

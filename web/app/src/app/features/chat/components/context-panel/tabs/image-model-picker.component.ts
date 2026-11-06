import { ChangeDetectionStrategy, Component, ElementRef, Injector, afterNextRender, inject, signal, viewChild } from '@angular/core';

import { AnnouncerService } from '../../../../../core/services/announcer.service';
import { listenForOutsideClick } from '../../../../../shared/ui';

interface ImageModelOption {
  readonly id: string;
  readonly label: string;
}

/**
 * Placeholder catalog: the backend has no image-model list or per-user
 * preference yet (it uses one fixed engine), so the choice is kept in this
 * browser only until that API exists.
 */
const IMAGE_MODELS: readonly ImageModelOption[] = [
  { id: 'gpt-image-1', label: 'GPT Image 1' },
  { id: 'gpt-image-1-mini', label: 'GPT Image 1 Mini' },
  { id: 'gemini-flash-image', label: 'Gemini Flash Image' },
];

const STORAGE_KEY = 'whatiff.defaultImageModel';

/**
 * Dropdown for the default image model, shown under the Generate Image tool.
 * Closed, it shows the current default and a "default" pill. Open, each other
 * model carries a grayed "default" pill; picking a row makes it the default.
 * It is a listbox (not a native select) so the pills can sit beside the names.
 */
@Component({
  selector: 'app-image-model-picker',
  standalone: true,
  template: `
    <div class="picker">
      <button
        #trigger
        type="button"
        class="picker__trigger"
        data-testid="image-model-trigger"
        aria-haspopup="listbox"
        [attr.aria-label]="'Default image model: ' + current().label"
        [attr.aria-expanded]="open()"
        [attr.aria-controls]="open() ? listId : null"
        (click)="toggle()"
        (keydown)="onTriggerKey($event)"
      >
        <span class="picker__name">{{ current().label }}</span>
        <span class="pill pill--on" aria-hidden="true">default</span>
        <svg
          class="picker__chevron"
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
      </button>
      @if (open()) {
        <ul
          #list
          class="picker__list"
          role="listbox"
          tabindex="-1"
          aria-label="Default image model"
          [id]="listId"
          [attr.aria-activedescendant]="optionId(activeIndex())"
          (keydown)="onListKey($event)"
        >
          @for (model of models; track model.id; let i = $index) {
            <li
              role="option"
              class="picker__option"
              [id]="optionId(i)"
              [class.picker__option--active]="i === activeIndex()"
              [attr.aria-selected]="model.id === selected()"
              [attr.aria-label]="model.label + (model.id === selected() ? ', default' : '')"
              (click)="choose(model.id)"
            >
              <span class="picker__name">{{ model.label }}</span>
              <span class="pill" aria-hidden="true" [class.pill--on]="model.id === selected()" [class.pill--off]="model.id !== selected()"
                >default</span
              >
            </li>
          }
        </ul>
      }
    </div>
  `,
  styles: [
    `
      .picker {
        margin: 0.5rem 0 0 1.75rem;
      }

      .picker__trigger,
      .picker__option {
        align-items: center;
        display: flex;
        gap: 0.5rem;
      }

      .picker__trigger {
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 0.75rem;
        color: var(--color-text-primary);
        cursor: pointer;
        font-size: 0.8125rem;
        min-height: 2.25rem;
        padding: 0.25rem 0.75rem;
        text-align: left;
        width: 100%;
      }

      .picker__name {
        flex: 1;
        min-width: 0;
        overflow-wrap: anywhere;
      }

      .picker__chevron {
        color: var(--color-text-muted);
        flex: none;
      }

      .picker__list {
        background: var(--color-surface-base);
        border: 1px solid var(--color-border-base);
        border-radius: 0.75rem;
        list-style: none;
        margin: 0.25rem 0 0;
        padding: 0.25rem;
      }

      .picker__option {
        border-radius: 0.5rem;
        cursor: pointer;
        font-size: 0.8125rem;
        min-height: 2rem;
        padding: 0.25rem 0.5rem;
      }

      .picker__option--active,
      .picker__option:hover {
        background: color-mix(in srgb, var(--color-accent) 10%, transparent);
      }

      .picker__trigger:focus-visible,
      .picker__list:focus-visible {
        outline: 2px solid var(--color-accent);
        outline-offset: 2px;
      }

      .pill {
        border: 1px solid var(--color-border-base);
        border-radius: 999px;
        flex: none;
        font-size: 0.6875rem;
        font-weight: 600;
        line-height: 1;
        padding: 0.3125rem 0.625rem;
      }

      .pill--on {
        background: color-mix(in srgb, var(--color-accent) 14%, transparent);
        border-color: var(--color-accent);
        color: var(--color-accent);
      }

      /* Secondary text color (not muted + opacity): the pill is visible text, so it must keep 4.5:1. */
      .pill--off {
        color: var(--color-text-secondary);
      }

      .picker__option:hover .pill--off,
      .picker__option--active .pill--off {
        border-color: var(--color-accent);
        color: var(--color-accent);
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ImageModelPickerComponent {
  readonly models = IMAGE_MODELS;
  readonly selected = signal(readStored());
  readonly open = signal(false);
  readonly activeIndex = signal(0);
  readonly listId = 'image-model-list';

  private readonly trigger = viewChild<ElementRef<HTMLButtonElement>>('trigger');
  private readonly list = viewChild<ElementRef<HTMLUListElement>>('list');
  private readonly injector = inject(Injector);
  private readonly announcer = inject(AnnouncerService);

  constructor() {
    // Registers a document capture listener and removes it via DestroyRef when this picker is destroyed.
    listenForOutsideClick(() => this.open.set(false));
  }

  current(): ImageModelOption {
    return IMAGE_MODELS.find(m => m.id === this.selected()) ?? IMAGE_MODELS[0];
  }

  optionId(index: number): string {
    return `${this.listId}-opt-${index}`;
  }

  toggle(): void {
    if (this.open()) {
      this.close(false);
    } else {
      this.openList();
    }
  }

  choose(id: string): void {
    this.selected.set(id);
    try {
      window.localStorage.setItem(STORAGE_KEY, id);
    } catch {
      // Storage can be unavailable (private mode); the choice then lasts for this session only.
    }
    this.announcer.announce(`Default image model set to ${this.current().label}`);
    this.close(true);
  }

  onTriggerKey(event: KeyboardEvent): void {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      this.openList();
    }
  }

  onListKey(event: KeyboardEvent): void {
    const last = this.models.length - 1;
    switch (event.key) {
      case 'ArrowDown':
        this.activeIndex.set(Math.min(this.activeIndex() + 1, last));
        break;
      case 'ArrowUp':
        this.activeIndex.set(Math.max(this.activeIndex() - 1, 0));
        break;
      case 'Home':
        this.activeIndex.set(0);
        break;
      case 'End':
        this.activeIndex.set(last);
        break;
      case 'Enter':
      case ' ':
        this.choose(this.models[this.activeIndex()].id);
        break;
      case 'Escape':
        // The panel closes on Escape too; this one only closes the list.
        event.stopPropagation();
        this.close(true);
        break;
      case 'Tab':
        this.close(false);
        return;
      default:
        return;
    }
    event.preventDefault();
  }

  private openList(): void {
    this.activeIndex.set(
      Math.max(
        0,
        this.models.findIndex(m => m.id === this.selected()),
      ),
    );
    this.open.set(true);
    // The list is rendered by this change, so focus it once Angular has put it in the DOM.
    afterNextRender(() => this.list()?.nativeElement.focus(), { injector: this.injector });
  }

  private close(refocus: boolean): void {
    this.open.set(false);
    if (refocus) afterNextRender(() => this.trigger()?.nativeElement.focus(), { injector: this.injector });
  }
}

function readStored(): string {
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    if (stored && IMAGE_MODELS.some(m => m.id === stored)) return stored;
  } catch {
    // fall through to the default
  }
  return IMAGE_MODELS[0].id;
}

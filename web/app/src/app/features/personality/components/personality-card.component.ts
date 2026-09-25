import {
  ChangeDetectionStrategy,
  Component,
  computed,
  inject,
  input,
  output,
} from '@angular/core';

import { Personality } from '../../../core/models/personality.model';
import { ImageGalleryService } from '../../../core/services/image-gallery.service';
import { PersonaAccentScopeComponent } from '../picker/persona-accent-scope.component';
import { PersonaCoverComponent } from '../picker/persona-cover.component';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { personalityCoverUrl } from '../helpers/cover-image.helpers';
import { toPersonalityCardVm } from '../helpers/personality-vm.helpers';

export type PersonalityCardAction = 'open' | 'edit' | 'delete' | 'set-default';

/**
 * Renders a single personality as a vertical card on the personalities page.
 * Cards are wrapped in `persona-accent-scope` so the cover, badges, and
 * borders pick up the deterministic per-personality accent.
 */
@Component({
  selector: 'app-personality-card',
  standalone: true,
  imports: [PersonaAccentScopeComponent, PersonaCoverComponent, TooltipDirective],
  template: `
    <persona-accent-scope [personality]="personality()">
      <article
        class="personality-card group relative cursor-pointer overflow-hidden rounded-xl bg-(--color-surface-card) shadow-[var(--card-shadow)] outline-none transition duration-150 ease-out hover:-translate-y-1 hover:shadow-[var(--card-shadow-hover)] focus-within:-translate-y-1 focus-visible:ring-2 focus-visible:ring-[var(--persona-accent)]"
        [attr.aria-labelledby]="titleId"
        (click)="onEdit()"
      >
        <div class="relative">
          <persona-cover
            [name]="vm().name"
            [imageUrl]="vm().coverImageUrl"
            size="card"
          />

          <div class="absolute inset-x-0 top-3 flex items-center justify-between px-3">
            <span
              class="inline-flex items-center gap-1.5 rounded-[1.25rem] bg-black/45 px-2.5 py-1 text-xs font-medium text-white backdrop-blur-[8px]"
              [uiTooltip]="usageTitle()"
              placement="bottom"
            >
              <span
                class="h-[0.4375rem] w-[0.4375rem] shrink-0 rounded-full"
                style="background: var(--persona-accent);"
                aria-hidden="true"
              ></span>
              {{ vm().usageBadge }}
            </span>

            <div class="flex items-center gap-1.5">
              @if (vm().isDefault) {
                <span
                  class="inline-flex items-center rounded-md bg-[var(--persona-accent)]/85 px-2 py-1 text-[0.625rem] font-semibold uppercase tracking-wide text-white"
                  uiTooltip="New threads start with this personality"
                  placement="bottom"
                >Default</span>
              }
              @if (!vm().isDefault) {
                <button
                  type="button"
                  class="inline-flex h-7 w-7 items-center justify-center rounded-lg bg-black/35 text-[0.75rem] text-white/85 opacity-0 backdrop-blur-[8px] transition group-hover:opacity-100 hover:bg-black/55 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-white/70"
                  uiTooltip="Make default: new threads start with this personality"
                  placement="bottom"
                  [attr.aria-label]="'Make ' + vm().name + ' default'"
                  (click)="onAction('set-default', $event)"
                >
                  ★
                </button>
              }
            </div>
          </div>

          <div
            class="absolute inset-x-0 bottom-0 h-[5.5rem] border-t-2 border-[var(--persona-accent)] bg-black/40 px-4 pb-3.5 pt-3 backdrop-blur-[10px]"
          >
            <h3
              #cardTitle
              [id]="titleId"
              class="truncate text-[1.0625rem] font-bold leading-tight text-white"
              [uiTooltip]="vm().name"
              truncatedOnly
              [truncationTarget]="cardTitle"
            >{{ vm().name }}</h3>
            <p class="mt-1 line-clamp-2 text-xs leading-[1.45] text-white/85">
              {{ vm().systemPromptPreview || 'No system prompt set.' }}
            </p>
          </div>
        </div>
      </article>
    </persona-accent-scope>
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class PersonalityCardComponent {
  private readonly imageGallery = inject(ImageGalleryService);

  readonly personality = input.required<Personality>();
  readonly defaultPersonalityId = input<string | null>(null);

  readonly action = output<PersonalityCardAction>();

  readonly titleId = `personality-card-${randomId()}`;

  readonly vm = computed(() =>
    toPersonalityCardVm(this.personality(), {
      defaultPersonalityId: this.defaultPersonalityId(),
      coverImageUrl: personalityCoverUrl(
        this.personality(),
        [],
        this.imageGallery.getImageUrl.bind(this.imageGallery),
      ),
    }),
  );

  readonly usageTitle = computed(() => {
    const stats = this.vm().stats;
    const count = stats?.chat_count ?? 0;
    if (count === 0) return 'Not used in any threads yet';
    const threads = `Used in ${count} ${count === 1 ? 'thread' : 'threads'}`;
    const lastUsed = stats?.last_used_at ? new Date(stats.last_used_at) : null;
    return lastUsed && !Number.isNaN(lastUsed.getTime())
      ? `${threads}, last on ${lastUsed.toLocaleDateString()}`
      : threads;
  });

  onEdit(): void {
    this.action.emit('edit');
  }

  onAction(action: PersonalityCardAction, event: Event): void {
    event.stopPropagation();
    event.preventDefault();
    this.action.emit(action);
  }
}

function randomId(): string {
  return Math.random().toString(36).slice(2, 10);
}

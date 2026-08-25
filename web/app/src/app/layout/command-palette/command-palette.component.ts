import { DOCUMENT, NgComponentOutlet } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  OnDestroy,
  computed,
  effect,
  inject,
  signal,
  viewChild,
} from '@angular/core';
import { Router } from '@angular/router';

import {
  CommandPaletteService,
  PaletteCommand,
} from '../../core/services/command-palette.service';
import { SearchResult, SearchSection } from '../../core/models/search.model';
import {
  BodyScrollLockHandle,
  lockBodyScroll,
  releaseBodyScroll,
} from '../../shared/ui/helpers/body-scroll-lock.helpers';
import {
  FocusTrapHandle,
  createFocusTrap,
  releaseFocusTrap,
} from '../../shared/ui/helpers/focus-trap.helpers';
import { isEscapeKey } from '../../shared/ui/helpers/keyboard.helpers';
import {
  ChatIconComponent,
  ImageIconComponent,
  BoltIconComponent,
  BrainIconComponent,
  UsersIconComponent,
} from '../../shared/ui/icons/icons';

/**
 * Section labels rendered as the section header. Order is canonical and
 * mirrors `SECTION_ORDER` in `core/models/search.model.ts`.
 */
const SECTION_LABEL: Record<SearchSection['type'], string> = {
  chat: 'Threads',
  personality: 'Personalities',
  ritual: 'Skills',
  memory: 'Memories',
  image: 'Images',
};

const SECTION_ICON: Record<SearchSection['type'], any> = {
  chat: ChatIconComponent,
  personality: UsersIconComponent,
  ritual: BoltIconComponent,
  memory: BrainIconComponent,
  image: ImageIconComponent,
};

interface RowDescriptor {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
  readonly icon: any;
  readonly kind: 'command' | 'result';
  readonly commandId?: string;
  readonly result?: SearchResult;
}

@Component({
  selector: 'app-command-palette',
  standalone: true,
  imports: [NgComponentOutlet],
  templateUrl: './command-palette.component.html',
  styleUrl: './command-palette.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CommandPaletteComponent implements OnDestroy {
  readonly service = inject(CommandPaletteService);
  private readonly router = inject(Router);
  private readonly document = inject(DOCUMENT);

  private readonly dialogRef = viewChild<ElementRef<HTMLElement>>('dialog');
  private readonly inputRef = viewChild<ElementRef<HTMLInputElement>>('input');

  private focusTrap: FocusTrapHandle | null = null;
  private bodyLock: BodyScrollLockHandle | null = null;

  readonly selectedIndex = signal(0);

  /** Flat row list used for keyboard navigation and rendering. */
  readonly rows = computed<RowDescriptor[]>(() => {
    const out: RowDescriptor[] = [];
    let i = 0;
    for (const cmd of this.service.commandResults()) {
      out.push({
        id: `cmd-${i++}-${cmd.id}`,
        label: cmd.label,
        description: cmd.description,
        icon: cmd.icon,
        kind: 'command',
        commandId: cmd.id,
      });
    }
    for (const section of this.service.sections()) {
      for (const result of section.results) {
        out.push({
          id: `res-${i++}-${result.id}`,
          label: result.label,
          description: result.description,
          icon: SECTION_ICON[section.type],
          kind: 'result',
          result,
        });
      }
    }
    return out;
  });

  readonly sectionLabel = (type: SearchSection['type']) => SECTION_LABEL[type];

  readonly activeRowId = computed(() => {
    const list = this.rows();
    if (list.length === 0) return '';
    const idx = clamp(this.selectedIndex(), 0, list.length - 1);
    return list[idx]?.id ?? '';
  });

  constructor() {
    effect(() => {
      if (this.service.visible()) {
        this.bodyLock ??= lockBodyScroll(this.document.body);
        // Focus the input on next tick once the template renders.
        setTimeout(() => {
          const dialog = this.dialogRef()?.nativeElement;
          if (dialog && !this.focusTrap) {
            this.focusTrap = createFocusTrap(dialog);
          }
          this.inputRef()?.nativeElement.focus();
        }, 0);
      } else {
        this.release();
      }
    });

    // Reset selection when results change shape.
    effect(() => {
      // Reading rows() makes this run on every change.
      this.rows();
      this.selectedIndex.set(0);
    });
  }

  ngOnDestroy(): void {
    this.release();
  }

  onBackdropClick(): void {
    this.service.close();
  }

  onDialogClick(event: Event): void {
    event.stopPropagation();
  }

  onKeydown(event: KeyboardEvent): void {
    if (isEscapeKey(event)) {
      event.preventDefault();
      this.service.close();
      return;
    }
    const list = this.rows();
    if (list.length === 0) return;

    if (event.key === 'ArrowDown') {
      event.preventDefault();
      this.selectedIndex.set((this.selectedIndex() + 1) % list.length);
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      this.selectedIndex.set((this.selectedIndex() - 1 + list.length) % list.length);
    } else if (event.key === 'Enter') {
      event.preventDefault();
      const row = list[clamp(this.selectedIndex(), 0, list.length - 1)];
      this.activate(row);
    }
  }

  onInput(event: Event): void {
    const value = (event.target as HTMLInputElement).value;
    this.service.setQuery(value);
  }

  onRowClick(row: RowDescriptor): void {
    this.activate(row);
  }

  onRowHover(index: number): void {
    this.selectedIndex.set(index);
  }

  private activate(row: RowDescriptor): void {
    if (row.kind === 'command' && row.commandId) {
      this.service.runCommand(row.commandId);
      return;
    }
    if (row.kind === 'result' && row.result?.route) {
      this.service.close();
      this.router.navigateByUrl(row.result.route);
    }
  }

  private release(): void {
    releaseFocusTrap(this.focusTrap);
    releaseBodyScroll(this.bodyLock);
    this.focusTrap = null;
    this.bodyLock = null;
  }
}

function clamp(value: number, min: number, max: number): number {
  if (value < min) return min;
  if (value > max) return max;
  return value;
}

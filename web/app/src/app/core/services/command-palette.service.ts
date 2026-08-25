import {
  Injectable,
  OnDestroy,
  Signal,
  Type,
  computed,
  inject,
  signal,
} from '@angular/core';
import { Subscription, forkJoin, of } from 'rxjs';
import { catchError, map } from 'rxjs/operators';
import { Observable } from 'rxjs';

import {
  mergeSections,
  rankResults,
  RankableItem,
} from '../../layout/command-palette/command-palette.helpers';
import { SearchSection } from '../models/search.model';

/**
 * Action-style row shown above the server-driven sections in the palette.
 * `run()` is dispatched by the component when the user activates the row;
 * routing-only commands wrap `Router.navigate` themselves.
 */
export interface PaletteCommand {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
  readonly icon: Type<unknown>;
  /** Extra search keywords. Combined with `description` when ranking. */
  readonly keywords?: ReadonlyArray<string>;
  readonly run: () => unknown | Promise<unknown>;
}

/** Function that produces palette sections for a given query. */
export type CommandPaletteHandler = (query: string) => Observable<SearchSection[]>;

/**
 * Owns palette open/close state, the typed query, and the merged result
 * sections from registered handlers. Static commands are managed via
 * `registerCommand`; resource handlers via `register`.
 *
 * Debouncing lives here (single 200ms idle timer) so consumers can call
 * `setQuery` directly from a controlled input without owning their own timer.
 */
@Injectable({ providedIn: 'root' })
export class CommandPaletteService implements OnDestroy {
  /** Whether the palette overlay is open. */
  readonly visible = signal(false);
  /** Echoed input value (updates immediately for a responsive field). */
  readonly query = signal('');
  /** Server-driven sections in canonical order. */
  readonly sections = signal<ReadonlyArray<SearchSection>>([]);
  /** Whether handlers have an in-flight request. */
  readonly loading = signal(false);

  private readonly handlers = new Set<CommandPaletteHandler>();
  private readonly staticCommands = new Map<string, PaletteCommand>();
  private debounceTimer: ReturnType<typeof setTimeout> | null = null;
  private inflight: Subscription | null = null;

  /** Static commands matching the current query, ranked by relevance. Empty query returns all. */
  readonly commandResults: Signal<PaletteCommand[]> = computed(() => {
    const q = this.query().trim();
    const all = Array.from(this.staticCommands.values());
    if (!q) return all;
    const items: Array<{ cmd: PaletteCommand } & RankableItem> = all.map(cmd => ({
      cmd,
      label: cmd.label,
      description: [cmd.description ?? '', ...(cmd.keywords ?? [])]
        .filter(Boolean)
        .join(' '),
    }));
    return rankResults(items, q).map(scored => scored.cmd);
  });

  ngOnDestroy(): void {
    this.cancelTimers();
  }

  /** Opens the palette. Resets the query so each open starts fresh. */
  open(): void {
    this.query.set('');
    this.sections.set([]);
    this.loading.set(false);
    this.visible.set(true);
  }

  /** Closes the palette and cancels in-flight handler work. */
  close(): void {
    this.cancelTimers();
    this.visible.set(false);
  }

  /** Convenience for the global ⌘K binding. */
  toggle(): void {
    if (this.visible()) {
      this.close();
    } else {
      this.open();
    }
  }

  /**
   * Updates the query and schedules a debounced refresh of registered
   * handlers. The query signal updates synchronously so input fields stay
   * responsive; only network fan-out is delayed.
   */
  setQuery(value: string): void {
    this.query.set(value);
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
    }
    this.debounceTimer = setTimeout(() => {
      this.debounceTimer = null;
      this.runHandlers(value);
    }, 200);
  }

  /** Registers a search handler. Returns an idempotent disposer. */
  register(handler: CommandPaletteHandler): () => void {
    this.handlers.add(handler);
    return () => {
      this.handlers.delete(handler);
    };
  }

  /** Registers a static palette command. Returns an idempotent disposer. */
  registerCommand(command: PaletteCommand): () => void {
    this.staticCommands.set(command.id, command);
    return () => {
      this.staticCommands.delete(command.id);
    };
  }

  /** Runs a command by id and closes the palette. No-op for unknown ids. */
  runCommand(id: string): void {
    const command = this.staticCommands.get(id);
    if (!command) return;
    this.close();
    void command.run();
  }

  private runHandlers(query: string): void {
    this.inflight?.unsubscribe();
    this.inflight = null;

    const trimmed = query.trim();
    if (!trimmed || this.handlers.size === 0) {
      this.sections.set([]);
      this.loading.set(false);
      return;
    }

    this.loading.set(true);
    const observables = Array.from(this.handlers).map(handler =>
      handler(trimmed).pipe(catchError(() => of<SearchSection[]>([]))),
    );

    this.inflight = forkJoin(observables)
      .pipe(map(results => mergeSections(results.flat())))
      .subscribe({
        next: merged => {
          this.sections.set(merged);
          this.loading.set(false);
        },
        error: () => {
          this.sections.set([]);
          this.loading.set(false);
        },
      });
  }

  private cancelTimers(): void {
    if (this.debounceTimer) {
      clearTimeout(this.debounceTimer);
      this.debounceTimer = null;
    }
    this.inflight?.unsubscribe();
    this.inflight = null;
    this.loading.set(false);
  }
}

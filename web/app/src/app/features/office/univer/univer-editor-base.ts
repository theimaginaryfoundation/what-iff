import { DOCUMENT } from '@angular/common';
import {
  DestroyRef,
  Directive,
  ElementRef,
  afterNextRender,
  computed,
  effect,
  inject,
  input,
  output,
  signal,
  untracked,
  viewChild,
} from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { FileEditor } from '../../file-workspace/editors/file-editor';
import { FidelityReport, describeLosses } from '../shared/fidelity';
import { FUniver, Univer } from './univer-loader';

/** CommandType.MUTATION in Univer: the commands that change a document. */
export const MUTATION = 2;
/** How long after a unit is created its own start-up mutations are still ignored. */
const SETTLE_MS = 1000;
/** Mutations that move the view or the caret, not the content. */
const VIEW_ONLY_MUTATION = /scroll|selection|zoom|skeleton|active|viewport|freeze-render|render|cursor|focus/i;

/** Whether a key press would type, delete or paste content (navigation, copy and find are fine). */
export function changesContent(event: KeyboardEvent): boolean {
  const key = event.key;
  const mod = event.ctrlKey || event.metaKey;
  if (mod) return ['v', 'x', 'z', 'y', 'b', 'i', 'u'].includes(key.toLowerCase());
  if (key.length === 1) return true;
  return ['Enter', 'Backspace', 'Delete', 'Tab', 'F2'].includes(key);
}

export interface UniverKit {
  createUniver: (typeof import('@univerjs/presets'))['createUniver'];
  options(
    container: HTMLElement,
    opts?: { readOnly?: boolean; formulaWorker?: Worker | null },
  ): Parameters<(typeof import('@univerjs/presets'))['createUniver']>[0];
}

/** What a unit (a workbook, a document) gives back: its snapshot. */
export interface UniverUnit<TData> {
  save(): TData;
}

/**
 * The lifecycle every Univer-backed editor shares (ADR 0x043, office suite): parse the bytes with
 * the converter, create a Univer instance in the view, show what the converter could not carry,
 * mark dirty on content mutations, hand the snapshot back through the converter on save, and
 * dispose. Subclasses say which kit to load, how to parse, how to create the unit and how to
 * serialise. The same component is the viewer: `readOnly` defaults to true and the host sets
 * false for the editor role.
 */
@Directive()
export abstract class UniverEditorBase<TData, TUnit extends UniverUnit<TData>> implements FileEditor {
  protected readonly document = inject(DOCUMENT);
  private readonly hostEl = inject<ElementRef<HTMLElement>>(ElementRef);
  protected readonly container = viewChild.required<ElementRef<HTMLElement>>('container');

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
  readonly readOnly = input(true);

  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly error = signal<string | null>(null);
  readonly report = signal<FidelityReport | null>(null);
  readonly losses = computed<readonly string[]>(() => {
    const report = this.report();
    return report ? describeLosses(report) : [];
  });
  readonly warnings = computed<readonly string[]>(() => this.report()?.warnings.map(w => w.feature) ?? []);
  readonly noticeDismissed = signal(false);

  protected univer: Univer | null = null;
  protected univerAPI: FUniver | null = null;
  protected unit: TUnit | null = null;
  /** Set once the view exists; the effect waits for it before reading inputs. */
  private readonly rendered = signal(false);
  private generation = 0;
  private lastContent: Blob | null = null;
  /** Counts content changes, so a save can tell whether any landed after it started. */
  private mutations = 0;
  private readonly savedAt = new WeakMap<Blob, number>();
  private openedReadOnly: boolean | null = null;

  /** The thing being edited, for messages: "spreadsheet", "document". */
  abstract readonly noun: string;
  protected abstract loadKit(): Promise<UniverKit>;
  protected abstract parse(blob: Blob, file: FileAttachment): Promise<{ data: TData; report: FidelityReport }>;
  protected abstract createUnit(univerAPI: FUniver, data: TData, readOnly: boolean): Promise<TUnit> | TUnit;
  protected abstract serialize(data: TData, file: FileAttachment): Promise<Blob>;
  /** The createUniver options for this instance; a subclass can add to them (a worker, say). */
  protected univerOptions(kit: UniverKit, container: HTMLElement, readOnly: boolean): ReturnType<UniverKit['options']> {
    return kit.options(container, { readOnly });
  }
  /** Called whenever the instance is torn down, before a reopen and on destroy. */
  protected onDispose(): void {}

  constructor() {
    afterNextRender(() => this.rendered.set(true));
    effect(() => {
      if (!this.rendered()) return;
      const blob = this.blob();
      this.file();
      const readOnly = this.readOnly();
      // After a save the host hands back the bytes this editor produced; the instance already shows them.
      if (blob === this.lastContent && readOnly === this.openedReadOnly) return;
      untracked(() => void this.open(blob));
    });
    this.hostEl.nativeElement.addEventListener(
      'keydown',
      event => {
        if ((event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 's') {
          event.preventDefault();
          event.stopPropagation();
          if (!this.readOnly()) this.saveRequested.emit();
          return;
        }
        // Read-only is enforced here as well as in Univer: keys that would change content never reach it.
        if (this.readOnly() && changesContent(event)) {
          event.preventDefault();
          event.stopPropagation();
        }
      },
      { capture: true },
    );
    this.hostEl.nativeElement.addEventListener(
      'dblclick',
      event => {
        if (this.readOnly()) event.stopPropagation();
      },
      { capture: true },
    );
    inject(DestroyRef).onDestroy(() => {
      this.generation += 1;
      this.dispose();
    });
  }

  async content(): Promise<Blob> {
    const unit = this.unit;
    if (!unit) throw new Error(`The ${this.noun} is not loaded`);
    const mutations = this.mutations;
    const blob = await this.serialize(unit.save(), this.file());
    this.lastContent = blob;
    this.savedAt.set(blob, mutations);
    return blob;
  }

  markSaved(saved?: Blob): void {
    // A change made while the save was running is still unsaved.
    const at = saved && this.savedAt.get(saved);
    this.setDirty(at !== undefined && at !== this.mutations);
  }

  dismissNotice(): void {
    this.noticeDismissed.set(true);
  }

  /** Parses the bytes and (re)creates the Univer instance; a newer call wins over one still loading. */
  private async open(blob: Blob): Promise<void> {
    const generation = ++this.generation;
    this.dispose();
    this.state.set('loading');
    this.error.set(null);
    this.setDirty(false);
    try {
      const file = this.file();
      const [kit, parsed] = await Promise.all([this.loadKit(), this.parse(blob, file)]);
      if (generation !== this.generation) return;
      this.report.set(parsed.report);
      const readOnly = this.readOnly();
      this.openedReadOnly = readOnly;
      const { univer, univerAPI } = kit.createUniver(this.univerOptions(kit, this.container().nativeElement, readOnly));
      this.univer = univer;
      this.univerAPI = univerAPI;
      this.unit = await this.createUnit(univerAPI, parsed.data, readOnly);
      if (generation !== this.generation) return;
      // Univer runs mutations of its own while a unit settles (skeleton, selection, layout); only
      // what happens after that is the user's.
      let settled = false;
      setTimeout(() => (settled = true), SETTLE_MS);
      univerAPI.addEvent(univerAPI.Event.CommandExecuted, (event: { id?: string; type?: number }) => {
        if (readOnly || !settled || event.type !== MUTATION || VIEW_ONLY_MUTATION.test(event.id ?? '')) return;
        this.mutations += 1;
        this.setDirty(true);
      });
      this.state.set('ready');
    } catch (error) {
      if (generation !== this.generation) return;
      console.error(`The ${this.noun} could not be opened`, error);
      this.error.set(`This ${this.noun} could not be opened here. Download it to edit it elsewhere.`);
      this.state.set('error');
    }
  }

  private setDirty(value: boolean): void {
    if (this.dirty() === value) return;
    this.dirty.set(value);
    this.dirtyChange.emit(value);
  }

  private dispose(): void {
    this.unit = null;
    this.univerAPI = null;
    try {
      this.univer?.dispose();
    } catch {
      // A half-created instance can throw on dispose; nothing to keep.
    }
    this.univer = null;
    this.onDispose();
    const el = this.rendered() ? this.container().nativeElement : null;
    if (el) el.replaceChildren();
  }
}

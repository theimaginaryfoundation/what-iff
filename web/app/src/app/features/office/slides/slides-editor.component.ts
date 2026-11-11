import { useOfficeFonts } from '../shared/office-fonts';
import { DOCUMENT } from '@angular/common';
import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  ElementRef,
  afterNextRender,
  afterRenderEffect,
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
import { Deck, Paragraph, Slide, SlideElement, blankDeck } from '../pptx/deck-model';
import { deckToPptx } from '../pptx/pptx-export';
import { pptxToDeck } from '../pptx/pptx-import';
import { PPTX_PARSER, PPTX_WRITER } from '../pptx/pptx-loader';
import { FidelityReport, describeLosses } from '../shared/fidelity';
import { FidelityNoticeComponent } from '../shared/fidelity-notice.component';
import {
  DeckHistory,
  Order,
  addElement,
  addSlide,
  deleteSlide,
  duplicateElement,
  duplicateSlide,
  firstRunValue,
  moveSlide,
  newImage,
  newLine,
  newShape,
  newTextBox,
  removeElement,
  reorderElement,
  setParagraphs,
  styleParagraphs,
  styleRuns,
  updateElement,
  updateSlide,
} from './deck-commands';
import { SHAPE_PRESETS } from './shape-paths';
import { ElementGeometry, PX_PER_PT, SlideCanvasComponent } from './slide-canvas.component';
import { SlideComments } from './slide-comments';
import { CommentPeopleService } from '../comments/comment-people.service';
import { CommentsPanelComponent } from '../comments/comments-panel.component';
import { CommentsPanelState } from '../comments/comments-panel-state.service';
import { PptxComments, SlideCommentThread } from '../pptx/pptx-comments';

/** The slide's right-click menu: where it opened (in the editor's box), and on what. */
interface SlideContextMenu {
  left: number;
  top: number;
  /** A comment pin's thread, for "Reply to comment" and "Delete comment". */
  threadId?: string;
  /** Otherwise the shape clicked, if any, and the spot on the slide in points. */
  elementId?: string | null;
  point?: { x: number; y: number };
}

export const PPTX_MIME = 'application/vnd.openxmlformats-officedocument.presentationml.presentation';
const FONT_SIZES = [10, 12, 14, 18, 24, 28, 32, 40, 48, 60, 72];
const THUMB_WIDTH_PX = 148;

/**
 * The presentation editor (ADR 0x043, office suite): our own, over the deck model. A rail of
 * slides, the current slide on a canvas that scales to fit, a toolbar for text, shapes, lines,
 * pictures, arrangement and slide operations, speaker notes, undo and redo. Read-only it is a
 * viewer with keyboard navigation. Saving writes a .pptx through pptxgenjs.
 */
@Component({
  selector: 'app-slides-editor',
  standalone: true,
  imports: [SlideCanvasComponent, FidelityNoticeComponent, CommentsPanelComponent],
  templateUrl: './slides-editor.component.html',
  styleUrl: './slides-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SlidesEditorComponent implements FileEditor {
  private readonly document = inject(DOCUMENT);
  private readonly loadParser = inject(PPTX_PARSER);
  private readonly writer = inject(PPTX_WRITER);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);
  private readonly stage = viewChild<ElementRef<HTMLElement>>('stage');
  private readonly imageInput = viewChild<ElementRef<HTMLInputElement>>('imageInput');

  readonly file = input.required<FileAttachment>();
  readonly blob = input.required<Blob>();
  readonly readOnly = input(true);

  readonly dirtyChange = output<boolean>();
  readonly saveRequested = output<void>();

  readonly dirty = signal(false);
  readonly state = signal<'loading' | 'ready' | 'error'>('loading');
  readonly error = signal<string | null>(null);
  readonly report = signal<FidelityReport | null>(null);
  /** What the last save wrote without (a CSV keeps one sheet, the writers report what they drop). */
  protected readonly exportReport = signal<FidelityReport | null>(null);
  readonly losses = computed<readonly string[]>(() => {
    const all = [this.report(), this.exportReport()].flatMap(report => (report ? describeLosses(report) : []));
    return [...new Set(all)];
  });
  readonly warnings = computed<readonly string[]>(() => this.report()?.warnings.map(w => w.feature) ?? []);
  readonly noticeDismissed = signal(false);

  readonly deck = signal<Deck>(blankDeck('empty', 'Untitled'));
  readonly slideIndex = signal(0);
  readonly slide = computed<Slide>(() => this.deck().slides[Math.min(this.slideIndex(), this.deck().slides.length - 1)]);
  readonly selectedId = signal<string | null>(null);
  readonly selected = computed<SlideElement | null>(() => this.slide().elements.find(el => el.id === this.selectedId()) ?? null);
  readonly editingId = signal<string | null>(null);
  readonly notesOpen = signal(false);
  readonly fitScale = signal(1);
  readonly zoom = signal<number | null>(null);
  readonly scale = computed(() => this.zoom() ?? this.fitScale());
  readonly thumbScale = computed(() => THUMB_WIDTH_PX / (this.deck().size.width * PX_PER_PT));
  readonly canUndo = signal(false);
  readonly canRedo = signal(false);
  readonly shapePresets = SHAPE_PRESETS;
  readonly fontSizes = FONT_SIZES;
  readonly shapeMenuOpen = signal(false);

  readonly selectedIsText = computed(() => this.selected()?.kind === 'shape');
  readonly selectedBold = computed(() => !!firstRunValue(this.selected(), 'bold'));
  readonly selectedItalic = computed(() => !!firstRunValue(this.selected(), 'italic'));
  readonly selectedUnderline = computed(() => !!firstRunValue(this.selected(), 'underline'));
  readonly selectedFontSize = computed(() => firstRunValue(this.selected(), 'fontSize') ?? 18);
  readonly selectedColor = computed(() => firstRunValue(this.selected(), 'color') ?? '#000000');
  readonly selectedAlign = computed(() => {
    const el = this.selected();
    return el?.kind === 'shape' ? (el.text?.paragraphs[0]?.align ?? 'left') : 'left';
  });
  readonly selectedBulleted = computed(() => {
    const el = this.selected();
    return el?.kind === 'shape' ? !!el.text?.paragraphs[0]?.bullet : false;
  });
  readonly selectedFill = computed(() => {
    const el = this.selected();
    return el?.kind === 'shape' && el.fill?.type === 'color' ? el.fill.color : '#FFFFFF';
  });
  readonly selectedBorder = computed(() => {
    const el = this.selected();
    if (el?.kind === 'shape' || el?.kind === 'line' || el?.kind === 'image') return el.border?.color ?? '#000000';
    return '#000000';
  });
  readonly slideBackground = computed(() => {
    const bg = this.slide().background;
    return bg?.type === 'color' ? bg.color : '#FFFFFF';
  });

  // --- comments (ADR 0x045) ------------------------------------------------------------------------
  readonly me = inject(CommentPeopleService).me;
  readonly comments = new SlideComments(this.deck, this.slide, this.me, () => this.setDirty(true));
  private readonly panel = inject(CommentsPanelState);
  /** The panel opens by itself on a deck with open threads, until the user opens or closes it. */
  readonly commentsOpen = computed(() => this.panel.open() ?? this.comments.openCount() > 0);
  /** The comments the file was read with; a save leaves a slide's comment part alone while its threads match. */
  private importedComments: PptxComments | null = null;
  private readonly savedComments = new WeakMap<Blob, readonly SlideCommentThread[]>();

  private readonly history = new DeckHistory();
  private readonly rendered = signal(false);
  private generation = 0;
  private lastContent: Blob | null = null;
  private readonly savedDecks = new WeakMap<Blob, Deck>();
  /** The bytes the deck was read from and the deck as read: a save patches these rather than rebuilding the file. */
  private original: Uint8Array | null = null;
  private imported: Deck | null = null;
  private resizeObserver: ResizeObserver | null = null;

  constructor() {
    void useOfficeFonts();
    afterNextRender(() => this.rendered.set(true));
    // The stage exists only once a deck is shown; watch it from then on and fit the slide to it.
    afterRenderEffect(() => {
      const stage = this.stage()?.nativeElement ?? null;
      if (stage !== this.observedStage) this.observeStage(stage);
    });
    effect(() => {
      if (!this.rendered()) return;
      const blob = this.blob();
      this.file();
      // After a save the host hands back the bytes this editor produced; the deck already is them.
      if (blob === this.lastContent) return;
      untracked(() => void this.open(blob));
    });
    this.host.nativeElement.addEventListener('keydown', event => this.onKeydown(event));
    inject(DestroyRef).onDestroy(() => {
      this.generation += 1;
      this.resizeObserver?.disconnect();
    });
  }

  // --- FileEditor --------------------------------------------------------------------------------

  async content(): Promise<Blob> {
    const deck = this.deck();
    const threads = this.comments.threads();
    const { bytes, report } = await deckToPptx(deck, {
      original: this.original,
      imported: this.imported,
      factory: this.writer,
      comments: threads,
      importedComments: this.importedComments,
    });
    this.exportReport.set(report);
    const blob = new Blob([bytes as BlobPart], { type: PPTX_MIME });
    this.lastContent = blob;
    this.savedDecks.set(blob, deck);
    this.savedComments.set(blob, threads);
    // Every save patches the file as it was opened, against the deck read from it. Patching what an
    // earlier save wrote would not work: shapes added since opening have no id in that file, so they
    // would be inserted again, and shapes deleted after that save would stay.
    return blob;
  }

  markSaved(saved?: Blob): void {
    // The deck is immutable, so a change made while the save was running is a different object.
    const deck = saved && this.savedDecks.get(saved);
    const threads = saved && this.savedComments.get(saved);
    this.setDirty(deck !== undefined && (deck !== this.deck() || threads !== this.comments.threads()));
  }

  dismissNotice(): void {
    this.noticeDismissed.set(true);
  }

  // --- loading -----------------------------------------------------------------------------------

  private async open(blob: Blob): Promise<void> {
    const generation = ++this.generation;
    this.state.set('loading');
    this.error.set(null);
    this.history.clear();
    this.updateHistoryFlags();
    this.setDirty(false);
    try {
      const file = this.file();
      const parse = await this.loadParser();
      const bytes = new Uint8Array(await blob.arrayBuffer());
      const result = await pptxToDeck(bytes, { id: `deck-${file.id}`, name: file.name }, parse);
      if (generation !== this.generation) return;
      this.report.set(result.report);
      this.original = bytes;
      this.imported = result.deck;
      this.importedComments = result.comments;
      this.deck.set(result.deck);
      this.comments.load(result.comments.threads);
      this.slideIndex.set(0);
      this.selectedId.set(null);
      this.editingId.set(null);
      this.state.set('ready');
      this.fit();
      // Keyboard navigation and shortcuts listen on the host, so give it focus once it is shown.
      setTimeout(() => this.stage()?.nativeElement.focus({ preventScroll: true }));
    } catch (error) {
      if (generation !== this.generation) return;
      console.error('The presentation could not be opened', error);
      this.error.set('This presentation could not be opened here. Download it to edit it elsewhere.');
      this.state.set('error');
    }
  }

  private observedStage: HTMLElement | null = null;

  private observeStage(stage: HTMLElement | null): void {
    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
    this.observedStage = stage;
    if (!stage) return;
    if (typeof ResizeObserver !== 'undefined') {
      this.resizeObserver = new ResizeObserver(() => this.fit());
      this.resizeObserver.observe(stage);
    }
    this.fit();
  }

  /** The scale at which the slide fills the stage with a margin. */
  fit(): void {
    const stage = this.stage()?.nativeElement;
    if (!stage) return;
    const { width, height } = this.deck().size;
    const availW = stage.clientWidth - 32;
    const availH = stage.clientHeight - 32;
    if (availW <= 0 || availH <= 0) return;
    this.fitScale.set(Math.max(0.05, Math.min(availW / (width * PX_PER_PT), availH / (height * PX_PER_PT))));
  }

  zoomBy(factor: number): void {
    this.zoom.set(Math.max(0.1, Math.min(4, this.scale() * factor)));
  }

  setZoomPercent(percent: number): void {
    if (!Number.isFinite(percent)) return;
    this.zoom.set(Math.max(0.1, Math.min(4, percent / 100)));
  }

  zoomToFit(): void {
    this.zoom.set(null);
    this.fit();
  }

  // --- slides ------------------------------------------------------------------------------------

  goTo(index: number): void {
    const n = this.deck().slides.length;
    if (!n) return;
    this.slideIndex.set(Math.max(0, Math.min(index, n - 1)));
    this.selectedId.set(null);
    this.editingId.set(null);
    this.comments.slideChanged();
  }

  // --- comments ----------------------------------------------------------------------------------

  toggleComments(): void {
    this.panel.open.set(!this.commentsOpen());
  }

  closeComments(): void {
    this.panel.open.set(false);
  }

  /** Starts a thread on the selected element, or on the slide when nothing is selected. */
  startComment(): void {
    if (this.readOnly()) return;
    this.comments.start(this.selected(), this.slideIndex() + 1);
    this.panel.open.set(true);
  }

  /** The right-click menu, where it was opened and what it offers. */
  readonly contextMenu = signal<SlideContextMenu | null>(null);
  /** Opens a thread's reply box in the panel (the right-click menu's "Reply to comment"). */
  readonly replyRequest = signal<{ threadId: string } | null>(null);

  /**
   * The slide's right-click menu, while editing: on a comment pin, reply to or delete its thread;
   * anywhere else, start a thread on the shape there, or on the slide at that spot.
   */
  onStageContextMenu(event: MouseEvent): void {
    if (this.readOnly() || this.editingId()) return;
    const target = event.target as Element;
    const slideEl = target.closest('.slide');
    if (!slideEl) return;
    event.preventDefault();
    const host = this.host.nativeElement.getBoundingClientRect();
    const at = { left: event.clientX - host.left, top: event.clientY - host.top };
    const threadId = target.closest('.slide__pin')?.getAttribute('data-thread-id');
    if (threadId && threadId !== 'draft') {
      this.contextMenu.set({ ...at, threadId });
      return;
    }
    const elementId = target.closest('[data-element-id]:not(.slide__element--layout)')?.getAttribute('data-element-id') ?? null;
    const rect = slideEl.getBoundingClientRect();
    const perPt = PX_PER_PT * this.scale();
    this.contextMenu.set({ ...at, elementId, point: { x: (event.clientX - rect.left) / perPt, y: (event.clientY - rect.top) / perPt } });
  }

  closeContextMenu(): void {
    this.contextMenu.set(null);
  }

  menuNewComment(): void {
    const menu = this.contextMenu();
    this.closeContextMenu();
    if (!menu || this.readOnly()) return;
    const element = menu.elementId ? (this.slide().elements.find(e => e.id === menu.elementId) ?? null) : null;
    if (element) this.selectedId.set(element.id);
    this.comments.start(element, this.slideIndex() + 1, element ? undefined : menu.point);
    this.panel.open.set(true);
  }

  menuReply(): void {
    const threadId = this.contextMenu()?.threadId;
    this.closeContextMenu();
    if (!threadId) return;
    this.comments.select(threadId);
    this.panel.open.set(true);
    this.replyRequest.set({ threadId });
  }

  menuDelete(): void {
    const threadId = this.contextMenu()?.threadId;
    this.closeContextMenu();
    if (threadId) this.comments.remove({ threadId, commentId: threadId });
  }

  /** A pin on the slide was chosen: its thread becomes active, and the panel shows it. */
  onPinSelect(threadId: string): void {
    if (threadId === 'draft') return;
    this.comments.select(threadId);
    this.panel.open.set(true);
  }

  addSlide(): void {
    const { deck, slide } = addSlide(this.deck(), this.slideIndex());
    this.commit(deck);
    this.goTo(deck.slides.indexOf(slide));
  }

  duplicateSlide(): void {
    const { deck, slide } = duplicateSlide(this.deck(), this.slide().id);
    this.commit(deck);
    this.goTo(deck.slides.indexOf(slide));
  }

  deleteSlide(): void {
    const index = this.slideIndex();
    this.commit(deleteSlide(this.deck(), this.slide().id));
    this.goTo(Math.min(index, this.deck().slides.length - 1));
  }

  moveSlide(delta: number): void {
    const to = this.slideIndex() + delta;
    if (to < 0 || to >= this.deck().slides.length) return;
    this.commit(moveSlide(this.deck(), this.slide().id, to));
    this.goTo(to);
  }

  setBackground(color: string): void {
    this.commit(updateSlide(this.deck(), this.slide().id, { background: { type: 'color', color: color.toUpperCase() } }));
  }

  setNotes(notes: string): void {
    this.commit(updateSlide(this.deck(), this.slide().id, { notes }), { history: false });
  }

  // --- elements ----------------------------------------------------------------------------------

  insertTextBox(): void {
    const { width, height } = this.deck().size;
    const el = newTextBox(width * 0.1, height * 0.4, width * 0.8, height * 0.15, 'New text');
    this.commit(addElement(this.deck(), this.slide().id, el));
    this.selectedId.set(el.id);
  }

  insertShape(type: string): void {
    this.shapeMenuOpen.set(false);
    const { width, height } = this.deck().size;
    const el = newShape(type, width * 0.3, height * 0.3, width * 0.3, height * 0.3, this.deck().themeColors[0] ?? '#4472C4');
    this.commit(addElement(this.deck(), this.slide().id, el));
    this.selectedId.set(el.id);
  }

  insertLine(): void {
    const { width, height } = this.deck().size;
    const el = newLine(width * 0.2, height * 0.5, width * 0.6, 0, '#333333');
    this.commit(addElement(this.deck(), this.slide().id, el));
    this.selectedId.set(el.id);
  }

  pickImage(): void {
    this.imageInput()?.nativeElement.click();
  }

  async onImageChosen(event: Event): Promise<void> {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (!file) return;
    const src = await readAsDataUrl(file);
    const { width, height } = await imageSize(src, this.document);
    const el = newImage(src, width, height, this.deck().size);
    this.commit(addElement(this.deck(), this.slide().id, el));
    this.selectedId.set(el.id);
  }

  deleteSelected(): void {
    const id = this.selectedId();
    if (!id) return;
    this.commit(removeElement(this.deck(), this.slide().id, id));
    this.selectedId.set(null);
  }

  duplicateSelected(): void {
    const id = this.selectedId();
    if (!id) return;
    const { deck, element } = duplicateElement(this.deck(), this.slide().id, id);
    this.commit(deck);
    if (element) this.selectedId.set(element.id);
  }

  reorder(order: Order): void {
    const id = this.selectedId();
    if (!id) return;
    this.commit(reorderElement(this.deck(), this.slide().id, id, order));
  }

  nudge(dx: number, dy: number): void {
    const el = this.selected();
    if (!el) return;
    this.commit(updateElement(this.deck(), this.slide().id, el.id, { left: el.left + dx, top: el.top + dy }));
  }

  onGestureStart(): void {
    this.history.push(this.deck());
    this.updateHistoryFlags();
  }

  onRotationChange(change: { id: string; rotate: number }): void {
    this.commit(updateElement(this.deck(), this.slide().id, change.id, { rotate: change.rotate }), { history: false });
  }

  onGeometryChange(change: { id: string; geometry: ElementGeometry }): void {
    this.commit(updateElement(this.deck(), this.slide().id, change.id, change.geometry), { history: false });
  }

  onSelect(id: string | null): void {
    if (this.editingId() && this.editingId() !== id) this.editingId.set(null);
    this.selectedId.set(id);
    this.shapeMenuOpen.set(false);
  }

  onRequestEdit(id: string): void {
    if (this.readOnly()) return;
    this.selectedId.set(id);
    this.editingId.set(id);
  }

  onTextCommit(commit: { id: string; paragraphs: Paragraph[]; cell?: { row: number; col: number } }): void {
    const current = this.slide().elements.find(el => el.id === commit.id);
    if (!current) return;
    let next: SlideElement | null = null;
    if (commit.cell && current.kind === 'table') {
      const rows = current.rows.map((row, r) =>
        row.map((cell, c) =>
          r === commit.cell!.row && c === commit.cell!.col ? { ...cell, text: { ...cell.text, paragraphs: commit.paragraphs } } : cell,
        ),
      );
      next = { ...current, rows };
    } else if (current.kind === 'shape') {
      next = setParagraphs(current, commit.paragraphs);
    }
    if (!next || JSON.stringify(next) === JSON.stringify(current)) {
      if (!commit.cell) this.editingId.set(null);
      return;
    }
    this.commit(updateElement(this.deck(), this.slide().id, commit.id, next));
    if (!commit.cell) this.editingId.set(null);
  }

  // --- formatting --------------------------------------------------------------------------------

  toggleRunStyle(key: 'bold' | 'italic' | 'underline'): void {
    if (this.editingId()) {
      this.document.execCommand(key, false);
      return;
    }
    const el = this.selected();
    if (!el) return;
    const value = !firstRunValue(el, key);
    this.commit(updateElement(this.deck(), this.slide().id, el.id, styleRuns(el, { [key]: value })));
  }

  setFontSize(size: number): void {
    const el = this.selected();
    if (!el) return;
    this.commit(updateElement(this.deck(), this.slide().id, el.id, styleRuns(el, { fontSize: size })));
  }

  setTextColor(color: string): void {
    const el = this.selected();
    if (!el) return;
    this.commit(updateElement(this.deck(), this.slide().id, el.id, styleRuns(el, { color: color.toUpperCase() })));
  }

  setAlign(align: Paragraph['align']): void {
    const el = this.selected();
    if (!el) return;
    this.commit(updateElement(this.deck(), this.slide().id, el.id, styleParagraphs(el, { align })));
  }

  toggleBullets(): void {
    const el = this.selected();
    if (!el || el.kind !== 'shape') return;
    const on = !el.text?.paragraphs[0]?.bullet;
    const next = {
      ...el,
      text: {
        ...(el.text ?? {
          paragraphs: [],
          vAlign: 'top' as const,
          inset: [7.2, 3.6, 7.2, 3.6] as [number, number, number, number],
          wrap: true,
          autoFit: 'none' as const,
        }),
      },
    };
    next.text.paragraphs = next.text.paragraphs.map(p => {
      const { bullet: _drop, ...rest } = p;
      void _drop;
      return on ? { ...rest, bullet: { type: 'bullet' as const, level: 0 } } : rest;
    });
    this.commit(updateElement(this.deck(), this.slide().id, el.id, next));
  }

  setFill(color: string): void {
    const el = this.selected();
    if (!el || el.kind !== 'shape') return;
    this.commit(updateElement(this.deck(), this.slide().id, el.id, { fill: { type: 'color', color: color.toUpperCase() } }));
  }

  clearFill(): void {
    const el = this.selected();
    if (!el || el.kind !== 'shape') return;
    this.commit(updateElement(this.deck(), this.slide().id, el.id, { fill: null }));
  }

  setBorder(color: string): void {
    const el = this.selected();
    if (!el || (el.kind !== 'shape' && el.kind !== 'line' && el.kind !== 'image')) return;
    const border = { color: color.toUpperCase(), width: el.border?.width ?? 1, style: el.border?.style ?? ('solid' as const) };
    this.commit(updateElement(this.deck(), this.slide().id, el.id, { border } as Partial<SlideElement>));
  }

  // --- history -----------------------------------------------------------------------------------

  undo(): void {
    const previous = this.history.undo(this.deck());
    if (!previous) return;
    this.deck.set(previous);
    this.afterRestore();
  }

  redo(): void {
    const next = this.history.redo(this.deck());
    if (!next) return;
    this.deck.set(next);
    this.afterRestore();
  }

  /** After undo or redo: stay on the slide and keep the selection when they still exist. */
  private afterRestore(): void {
    this.slideIndex.set(Math.max(0, Math.min(this.slideIndex(), this.deck().slides.length - 1)));
    if (!this.slide().elements.some(el => el.id === this.selectedId())) this.selectedId.set(null);
    this.editingId.set(null);
    this.updateHistoryFlags();
    this.setDirty(true);
  }

  /** Applies a new deck; by default the previous one goes on the undo stack. */
  private commit(deck: Deck, opts: { history?: boolean } = {}): void {
    if (deck === this.deck()) return;
    if (opts.history !== false) this.history.push(this.deck());
    this.deck.set(deck);
    this.updateHistoryFlags();
    this.setDirty(true);
  }

  private updateHistoryFlags(): void {
    this.canUndo.set(this.history.canUndo);
    this.canRedo.set(this.history.canRedo);
  }

  private setDirty(value: boolean): void {
    if (this.dirty() === value) return;
    this.dirty.set(value);
    this.dirtyChange.emit(value);
  }

  // --- keyboard ----------------------------------------------------------------------------------

  private onKeydown(event: KeyboardEvent): void {
    const target = event.target as HTMLElement;
    const typing = target.isContentEditable || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT';
    const mod = event.ctrlKey || event.metaKey;
    if (mod && event.altKey && event.key.toLowerCase() === 'm') {
      event.preventDefault();
      this.startComment();
      return;
    }
    if (mod && !event.shiftKey && event.key.toLowerCase() === 's') {
      event.preventDefault();
      if (!this.readOnly()) this.saveRequested.emit();
      return;
    }
    if (typing) return;
    if (this.readOnly()) {
      if (event.key === 'ArrowRight' || event.key === 'ArrowDown' || event.key === 'PageDown' || event.key === ' ') {
        event.preventDefault();
        this.goTo(this.slideIndex() + 1);
      } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp' || event.key === 'PageUp') {
        event.preventDefault();
        this.goTo(this.slideIndex() - 1);
      } else if (event.key === 'Home') this.goTo(0);
      else if (event.key === 'End') this.goTo(this.deck().slides.length - 1);
      return;
    }
    if (mod && event.key.toLowerCase() === 'z') {
      event.preventDefault();
      if (event.shiftKey) this.redo();
      else this.undo();
    } else if (mod && event.key.toLowerCase() === 'y') {
      event.preventDefault();
      this.redo();
    } else if (mod && event.key.toLowerCase() === 'd' && this.selectedId()) {
      event.preventDefault();
      this.duplicateSelected();
    } else if ((event.key === 'Delete' || event.key === 'Backspace') && this.selectedId()) {
      event.preventDefault();
      this.deleteSelected();
    } else if (event.key === 'Escape') {
      this.editingId.set(null);
      this.selectedId.set(null);
      this.shapeMenuOpen.set(false);
    } else if (event.key.startsWith('Arrow') && this.selectedId()) {
      event.preventDefault();
      const step = event.shiftKey ? 10 : 1;
      this.nudge(
        event.key === 'ArrowLeft' ? -step : event.key === 'ArrowRight' ? step : 0,
        event.key === 'ArrowUp' ? -step : event.key === 'ArrowDown' ? step : 0,
      );
    } else if (event.key === 'PageDown') {
      event.preventDefault();
      this.goTo(this.slideIndex() + 1);
    } else if (event.key === 'PageUp') {
      event.preventDefault();
      this.goTo(this.slideIndex() - 1);
    }
  }

  openLink(url: string): void {
    if (/^https?:\/\//i.test(url)) this.document.defaultView?.open(url, '_blank', 'noopener');
  }
}

function readAsDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result));
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

function imageSize(src: string, document: Document): Promise<{ width: number; height: number }> {
  return new Promise(resolve => {
    const img = document.createElement('img');
    img.onload = () => resolve({ width: img.naturalWidth || 300, height: img.naturalHeight || 200 });
    img.onerror = () => resolve({ width: 300, height: 200 });
    img.src = src;
  });
}

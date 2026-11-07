import {
  Deck,
  DEFAULT_INSET,
  ImageElement,
  LineElement,
  Paragraph,
  ShapeElement,
  Slide,
  SlideElement,
  TextBody,
  blankSlide,
  emptyTextBody,
  newId,
} from '../pptx/deck-model';

/**
 * The edits the Slides editor makes, as pure functions over the deck (a new deck each time, the
 * untouched slides shared), plus the undo history. Positions and sizes are points.
 */

export function updateSlide(deck: Deck, slideId: string, patch: Partial<Slide> | ((slide: Slide) => Slide)): Deck {
  return { ...deck, slides: deck.slides.map(s => (s.id === slideId ? (typeof patch === 'function' ? patch(s) : { ...s, ...patch }) : s)) };
}

export function addSlide(deck: Deck, afterIndex: number): { deck: Deck; slide: Slide } {
  const slide = blankSlide();
  const slides = deck.slides.slice();
  slides.splice(Math.min(afterIndex + 1, slides.length), 0, slide);
  return { deck: { ...deck, slides }, slide };
}

export function duplicateSlide(deck: Deck, slideId: string): { deck: Deck; slide: Slide } {
  const index = deck.slides.findIndex(s => s.id === slideId);
  const source = deck.slides[index];
  const copy: Slide = { ...structuredClone(source), id: newId('slide') };
  const reId = (els: SlideElement[]): SlideElement[] =>
    els.map(el => ({ ...el, id: newId('el'), ...(el.kind === 'group' ? { children: reId(el.children) } : {}) }) as SlideElement);
  copy.elements = reId(copy.elements);
  const slides = deck.slides.slice();
  slides.splice(index + 1, 0, copy);
  return { deck: { ...deck, slides }, slide: copy };
}

export function deleteSlide(deck: Deck, slideId: string): Deck {
  const slides = deck.slides.filter(s => s.id !== slideId);
  return { ...deck, slides: slides.length ? slides : [blankSlide()] };
}

export function moveSlide(deck: Deck, slideId: string, toIndex: number): Deck {
  const from = deck.slides.findIndex(s => s.id === slideId);
  if (from === -1) return deck;
  const slides = deck.slides.slice();
  const [slide] = slides.splice(from, 1);
  slides.splice(Math.max(0, Math.min(toIndex, slides.length)), 0, slide);
  return { ...deck, slides };
}

export function addElement(deck: Deck, slideId: string, element: SlideElement): Deck {
  return updateSlide(deck, slideId, s => ({ ...s, elements: [...s.elements, element] }));
}

export function updateElement(
  deck: Deck,
  slideId: string,
  elementId: string,
  patch: Partial<SlideElement> | ((el: SlideElement) => SlideElement),
): Deck {
  return updateSlide(deck, slideId, s => ({
    ...s,
    elements: s.elements.map(el =>
      el.id === elementId ? (typeof patch === 'function' ? patch(el) : ({ ...el, ...patch } as SlideElement)) : el,
    ),
  }));
}

export function removeElement(deck: Deck, slideId: string, elementId: string): Deck {
  return updateSlide(deck, slideId, s => ({ ...s, elements: s.elements.filter(el => el.id !== elementId) }));
}

export function duplicateElement(deck: Deck, slideId: string, elementId: string): { deck: Deck; element: SlideElement | null } {
  const slide = deck.slides.find(s => s.id === slideId);
  const source = slide?.elements.find(el => el.id === elementId);
  if (!source) return { deck, element: null };
  const element = { ...structuredClone(source), id: newId('el'), left: source.left + 12, top: source.top + 12 } as SlideElement;
  return { deck: addElement(deck, slideId, element), element };
}

export type Order = 'forward' | 'backward' | 'front' | 'back';

export function reorderElement(deck: Deck, slideId: string, elementId: string, order: Order): Deck {
  return updateSlide(deck, slideId, s => {
    const index = s.elements.findIndex(el => el.id === elementId);
    if (index === -1) return s;
    const elements = s.elements.slice();
    const [el] = elements.splice(index, 1);
    const to =
      order === 'front'
        ? elements.length
        : order === 'back'
          ? 0
          : order === 'forward'
            ? Math.min(index + 1, elements.length)
            : Math.max(index - 1, 0);
    elements.splice(to, 0, el);
    return { ...s, elements };
  });
}

// --- new elements ---------------------------------------------------------------------------

const base = (name: string, left: number, top: number, width: number, height: number) => ({
  id: newId('el'),
  name,
  left,
  top,
  width,
  height,
  rotate: 0,
  flipH: false,
  flipV: false,
});

export function newTextBox(left: number, top: number, width: number, height: number, text = ''): ShapeElement {
  const body: TextBody = emptyTextBody([{ align: 'left', runs: text ? [{ text, fontSize: 18 }] : [] }]);
  return {
    ...base('Text', left, top, width, height),
    kind: 'shape',
    shapeType: 'rect',
    fill: null,
    border: null,
    text: body,
    isTextBox: true,
  };
}

export function newShape(shapeType: string, left: number, top: number, width: number, height: number, color: string): ShapeElement {
  return {
    ...base('Shape', left, top, width, height),
    kind: 'shape',
    shapeType,
    fill: { type: 'color', color },
    border: { color: darken(color), width: 1, style: 'solid' },
    text: { ...emptyTextBody([]), vAlign: 'middle', inset: [...DEFAULT_INSET] as [number, number, number, number] },
    isTextBox: false,
  };
}

export function newLine(left: number, top: number, width: number, height: number, color: string): LineElement {
  return {
    ...base('Line', left, top, width, height),
    kind: 'line',
    border: { color, width: 1.5, style: 'solid' },
    headEnd: { type: 'none' },
    tailEnd: { type: 'none' },
  };
}

/** An image sized to fit within the slide, centred, keeping its aspect ratio. */
export function newImage(src: string, naturalWidth: number, naturalHeight: number, slide: { width: number; height: number }): ImageElement {
  const maxW = slide.width * 0.6;
  const maxH = slide.height * 0.6;
  const scale = Math.min(1, maxW / Math.max(1, naturalWidth), maxH / Math.max(1, naturalHeight));
  const width = Math.max(10, Math.round(naturalWidth * scale));
  const height = Math.max(10, Math.round(naturalHeight * scale));
  return {
    ...base('Picture', Math.round((slide.width - width) / 2), Math.round((slide.height - height) / 2), width, height),
    kind: 'image',
    src,
    border: null,
    geometry: 'rect',
  };
}

export function darken(hex: string): string {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex);
  if (!m) return '#333333';
  const n = parseInt(m[1], 16);
  const ch = (v: number) =>
    Math.max(0, Math.round(v * 0.65))
      .toString(16)
      .padStart(2, '0');
  return `#${ch((n >> 16) & 255)}${ch((n >> 8) & 255)}${ch(n & 255)}`.toUpperCase();
}

// --- text helpers ------------------------------------------------------------------------------

export function setParagraphs(element: SlideElement, paragraphs: Paragraph[]): SlideElement {
  if (element.kind !== 'shape') return element;
  const text = element.text ?? emptyTextBody([]);
  return { ...element, text: { ...text, paragraphs } };
}

/** Applies a run property to every run of the element's text (and to empty text as a default run). */
export function styleRuns(element: SlideElement, patch: Partial<Omit<Paragraph['runs'][number], 'text'>>): SlideElement {
  if (element.kind !== 'shape' || !element.text) return element;
  const paragraphs = element.text.paragraphs.map(p => ({
    ...p,
    runs: p.runs.length ? p.runs.map(r => ({ ...r, ...patch })) : [{ text: '', ...patch }],
  }));
  return { ...element, text: { ...element.text, paragraphs } };
}

export function styleParagraphs(element: SlideElement, patch: Partial<Omit<Paragraph, 'runs'>>): SlideElement {
  if (element.kind !== 'shape' || !element.text) return element;
  const paragraphs = element.text.paragraphs.length ? element.text.paragraphs : [{ align: 'left' as const, runs: [] }];
  return { ...element, text: { ...element.text, paragraphs: paragraphs.map(p => ({ ...p, ...patch })) } };
}

/** The first run's value of a property, to show the current state in a toolbar. */
export function firstRunValue<K extends keyof Paragraph['runs'][number]>(
  element: SlideElement | null,
  key: K,
): Paragraph['runs'][number][K] | undefined {
  if (!element || element.kind !== 'shape') return undefined;
  for (const p of element.text?.paragraphs ?? []) for (const run of p.runs) return run[key];
  return undefined;
}

// --- history ---------------------------------------------------------------------------------

/** Undo and redo over whole decks; slides are shared between versions, so this is cheap. */
export class DeckHistory {
  private past: Deck[] = [];
  private future: Deck[] = [];

  constructor(private readonly limit = 100) {}

  get canUndo(): boolean {
    return this.past.length > 0;
  }

  get canRedo(): boolean {
    return this.future.length > 0;
  }

  /** Records the state before an edit. */
  push(before: Deck): void {
    this.past.push(before);
    if (this.past.length > this.limit) this.past.shift();
    this.future = [];
  }

  undo(current: Deck): Deck | null {
    const previous = this.past.pop();
    if (!previous) return null;
    this.future.push(current);
    return previous;
  }

  redo(current: Deck): Deck | null {
    const next = this.future.pop();
    if (!next) return null;
    this.past.push(current);
    return next;
  }

  clear(): void {
    this.past = [];
    this.future = [];
  }
}

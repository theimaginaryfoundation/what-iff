import { describe, expect, it } from 'vitest';

import { blankDeck } from '../pptx/deck-model';
import {
  DeckHistory,
  addElement,
  addSlide,
  deleteSlide,
  duplicateElement,
  duplicateSlide,
  moveSlide,
  newShape,
  newTextBox,
  reorderElement,
  styleParagraphs,
  styleRuns,
  updateElement,
} from './deck-commands';
import { shapePath } from './shape-paths';
import { domToParagraphs, paragraphsToHtml } from './text-dom';

describe('deck commands', () => {
  it('adds, duplicates, moves and deletes slides without touching the others', () => {
    const deck = blankDeck('d', 'Deck');
    const first = deck.slides[0];
    const added = addSlide(deck, 0);
    expect(added.deck.slides.map(s => s.id)).toEqual([first.id, added.slide.id]);
    expect(added.deck.slides[0]).toBe(first);
    const withText = addElement(added.deck, first.id, newTextBox(10, 10, 100, 40, 'Hi'));
    const dup = duplicateSlide(withText, first.id);
    expect(dup.deck.slides).toHaveLength(3);
    expect(dup.slide.elements[0].id).not.toBe(withText.slides[0].elements[0].id);
    expect(dup.slide.elements[0]).toMatchObject({ kind: 'shape', left: 10 });
    const moved = moveSlide(dup.deck, dup.slide.id, 0);
    expect(moved.slides[0].id).toBe(dup.slide.id);
    const deleted = deleteSlide(deleteSlide(deleteSlide(moved, dup.slide.id), first.id), added.slide.id);
    expect(deleted.slides).toHaveLength(1);
    expect(deleted.slides[0].elements).toEqual([]);
  });

  it('edits, duplicates, reorders and styles elements', () => {
    let deck = blankDeck('d', 'Deck');
    const slide = deck.slides[0].id;
    const a = newShape('rect', 0, 0, 50, 50, '#FF0000');
    const b = newTextBox(0, 0, 100, 20, 'Text');
    deck = addElement(addElement(deck, slide, a), slide, b);
    deck = updateElement(deck, slide, a.id, { left: 5 });
    expect(deck.slides[0].elements[0]).toMatchObject({ left: 5, border: { color: '#A60000' } });
    deck = reorderElement(deck, slide, a.id, 'front');
    expect(deck.slides[0].elements.map(e => e.id)).toEqual([b.id, a.id]);
    deck = reorderElement(deck, slide, a.id, 'backward');
    expect(deck.slides[0].elements.map(e => e.id)).toEqual([a.id, b.id]);
    const dup = duplicateElement(deck, slide, b.id);
    expect(dup.element).toMatchObject({ left: 12, top: 12 });
    expect(dup.deck.slides[0].elements).toHaveLength(3);
    const styled = styleParagraphs(styleRuns(b, { bold: true, fontSize: 24 }), { align: 'center' });
    expect(styled.kind === 'shape' && styled.text?.paragraphs[0]).toMatchObject({
      align: 'center',
      runs: [{ text: 'Text', bold: true, fontSize: 24 }],
    });
  });

  it('undoes and redoes whole decks', () => {
    const history = new DeckHistory(2);
    const d0 = blankDeck('d', 'Deck');
    const d1 = addSlide(d0, 0).deck;
    const d2 = addSlide(d1, 1).deck;
    const d3 = addSlide(d2, 2).deck;
    history.push(d0);
    history.push(d1);
    history.push(d2);
    expect(history.undo(d3)).toBe(d2);
    expect(history.undo(d2)).toBe(d1);
    expect(history.canUndo).toBe(false);
    expect(history.redo(d1)).toBe(d2);
    expect(history.redo(d2)).toBe(d3);
    expect(history.canRedo).toBe(false);
  });
});

describe('shape paths', () => {
  it('draws presets in the element box and falls back to a rectangle', () => {
    expect(shapePath('rect', 100, 50)).toBe('M0,0H100V50H0Z');
    expect(shapePath('unknown', 100, 50)).toBe('M0,0H100V50H0Z');
    expect(shapePath('ellipse', 100, 50)).toContain('A50,25');
    expect(shapePath('roundRect', 100, 50, { adj: 0.2 })).toContain('M10,0');
    expect(shapePath('diamond', 100, 50)).toBe('M50,0L100,25L50,50L0,25Z');
    expect(shapePath('star5', 100, 100).split('L')).toHaveLength(10);
    expect(shapePath('line', 100, 0)).toBe('M0,0L100,0');
  });
});

describe('text dom', () => {
  it('renders paragraphs as editable HTML and reads them back', () => {
    const paragraphs = [
      {
        align: 'center' as const,
        runs: [
          { text: 'Hello ', bold: true, fontSize: 24 },
          { text: 'there', color: '#FF0000', italic: true },
        ],
      },
      { align: 'left' as const, bullet: { type: 'bullet' as const, level: 0 }, runs: [{ text: 'one' }] },
      { align: 'left' as const, bullet: { type: 'bullet' as const, level: 1 }, runs: [{ text: 'nested', underline: true }] },
      { align: 'left' as const, bullet: { type: 'number' as const, level: 0 }, runs: [{ text: 'first', link: 'https://x.y' }] },
    ];
    const html = paragraphsToHtml(paragraphs);
    expect(html).toContain('<p style="text-align: center"><span style="font-size: 24pt;font-weight: bold">Hello </span>');
    expect(html).toContain('<ul><li><p><span>one</span></p></li><ul><li>');
    expect(html).toContain('<ol><li><p><a href="https://x.y">');
    const root = document.createElement('div');
    root.innerHTML = html;
    expect(domToParagraphs(root)).toEqual([
      {
        align: 'center',
        runs: [
          { text: 'Hello ', bold: true, fontSize: 24 },
          { text: 'there', color: '#FF0000', italic: true },
        ],
      },
      { align: 'left', bullet: { type: 'bullet', level: 0 }, runs: [{ text: 'one' }] },
      { align: 'left', bullet: { type: 'bullet', level: 1 }, runs: [{ text: 'nested', underline: true }] },
      { align: 'left', bullet: { type: 'number', level: 0 }, runs: [{ text: 'first', link: 'https://x.y' }] },
    ]);
  });

  it('normalises what browsers leave behind while editing', () => {
    const root = document.createElement('div');
    root.innerHTML = '<div>typed<font color="#00ff00" size="5"> big</font></div>loose<p><b>bold</b></p>';
    expect(domToParagraphs(root)).toEqual([
      { align: 'left', runs: [{ text: 'typed' }, { text: ' big', color: '#00FF00', fontSize: 18 }] },
      { align: 'left', runs: [{ text: 'loose' }] },
      { align: 'left', runs: [{ text: 'bold', bold: true }] },
    ]);
  });
});

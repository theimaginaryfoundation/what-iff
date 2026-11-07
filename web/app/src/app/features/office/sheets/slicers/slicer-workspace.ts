import { signal } from '@angular/core';

import {
  AnchorMarker,
  SlicerAnchor,
  SlicerCacheModel,
  SlicerPartEntry,
  TableModel,
  emptySlicerPreserved,
  newPivotSlicerCacheXml,
  newSlicerPartXml,
  newTableSlicerCacheXml,
  parseSlicerCache,
  parseSlicerPart,
  parseTable,
  patchSlicerAnchor,
  slicerAnchorXml,
  slicerAnchors,
  updateSlicerCacheXml,
  updateTableFiltersXml,
} from '../../xlsx/xlsx-slicers';
import { PreservedParts, contentTypeFor } from '../../xlsx/xlsx-preserve';
import { BLANK_LABEL, compareLabels, itemLabel } from '../pivots/pivot-engine';
import { PivotWorkspace } from '../pivots/pivot-workspace';
import { SheetGrid } from '../sheet-grid';

const encoder = new TextEncoder();
const decoder = new TextDecoder();
const DEFAULT_ROW_HEIGHT_EMU = '241300';
const EMU_PER_PX = 9525;

export interface SlicerEntry {
  /** Unique among slicers: the name in the drawing and the slicer part. */
  name: string;
  caption: string;
  cacheName: string;
  field: string;
  kind: 'pivot' | 'table';
  sheet: string;
  anchor: SlicerAnchor | null;
  partPath: string;
  /** The pivots driven, by entry id. */
  pivotIds: string[];
  /** The table's path. */
  tablePath: string | null;
  /** The labels left out; none means nothing is filtered. */
  excluded: string[];
  isNew: boolean;
  dirty: boolean;
}

/** Updates a slicer's picture on the grid; absent in tests that do not care. */
export interface SlicerPictures {
  /** The last column any picture on the sheet reaches, or -1. */
  rightEdge?(sheet: string): number;
  update(entry: SlicerEntry, items: readonly { label: string; selected: boolean }[]): void;
  add(entry: SlicerEntry, items: readonly { label: string; selected: boolean }[]): void;
  remove(entry: SlicerEntry): void;
  /** Where the picture is now, if the reader moved it. */
  anchorOf?(entry: SlicerEntry): SlicerAnchor | null;
}

export function pictureId(entry: Pick<SlicerEntry, 'name'>): string {
  return `slicer-${entry.name.replace(/[^A-Za-z0-9_-]/g, '_')}`;
}

/**
 * The open workbook's slicers. A pivot-backed slicer drives its pivots through PivotWorkspace; a
 * table-backed one hides rows. State is written into the file's parts in flush, for what changed.
 */
export class SlicerWorkspace {
  readonly entries = signal<readonly SlicerEntry[]>([]);
  readonly tables: TableModel[] = [];
  private grid: SheetGrid | null = null;
  private pictures: SlicerPictures | null = null;
  private readonly caches = new Map<string, SlicerCacheModel>();
  private readonly removedNames: string[] = [];
  private readonly tableFiltersDirty = new Set<string>();

  constructor(
    private readonly preserved: PreservedParts,
    private readonly pivots: PivotWorkspace,
  ) {
    preserved.slicers ??= emptySlicerPreserved();
    const book = preserved.slicers;
    for (const part of Object.values(preserved.bySheet)) {
      for (const path of part.tables) {
        const xml = this.text(path);
        if (xml) this.tables.push(parseTable(xml, path));
      }
    }
    for (const path of book.slicerCaches) {
      const xml = this.text(path);
      if (!xml) continue;
      const cache = parseSlicerCache(xml, path);
      this.caches.set(cache.name, cache);
    }
    const entries: SlicerEntry[] = [];
    for (const [sheet, parts] of Object.entries(book.sheetSlicers)) {
      const drawingPath = preserved.bySheet[sheet]?.drawing?.path;
      const drawingXml = drawingPath ? this.text(drawingPath) : null;
      const anchors = drawingXml ? slicerAnchors(drawingXml) : new Map<string, SlicerAnchor>();
      for (const partPath of parts) {
        const xml = this.text(partPath);
        if (!xml) continue;
        for (const slicer of parseSlicerPart(xml)) {
          const cache = this.caches.get(slicer.cache);
          if (cache) entries.push(this.entryFrom(slicer, cache, sheet, partPath, anchors.get(slicer.name) ?? null));
        }
      }
    }
    this.entries.set(entries);
  }

  private entryFrom(slicer: SlicerPartEntry, cache: SlicerCacheModel, sheet: string, partPath: string, anchor: SlicerAnchor | null): SlicerEntry {
    let excluded: string[] = [];
    let tablePath: string | null = null;
    let pivotIds: string[] = [];
    if (cache.kind === 'table') {
      const table = this.tables.find(t => t.id === cache.tableId);
      tablePath = table?.path ?? null;
      const offset = table ? table.columns.findIndex(c => c.id === cache.column) : -1;
      if (table && offset >= 0 && table.filters[offset]) {
        // State lives in the table's filter; what is left out is worked out when the sheet opens.
        excluded = [`\u0000shown:${JSON.stringify(table.filters[offset])}`];
      }
    } else {
      pivotIds = this.pivots.entries().filter(e => cache.pivotTables.some(p => p.name === e.name)).map(e => e.id);
      const first = this.pivots.entries().find(e => pivotIds.includes(e.id));
      const field = first ? this.pivots.caches.get(first.cachePath)?.fields.find(f => f.name === cache.sourceName) : undefined;
      if (field) excluded = cache.items.filter(i => !i.selected).map(i => field.items[i.x]).filter((l): l is string => l !== undefined);
    }
    return {
      name: slicer.name,
      caption: slicer.caption,
      cacheName: cache.name,
      field: cache.sourceName,
      kind: cache.kind,
      sheet,
      anchor,
      partPath,
      pivotIds,
      tablePath,
      excluded,
      isNew: false,
      dirty: false,
    };
  }

  attach(grid: SheetGrid, pictures: SlicerPictures | null): void {
    this.grid = grid;
    this.pictures = pictures;
    // A table's filter names what shows; turn that into what is left out, against the live column.
    this.entries.update(list =>
      list.map(entry => {
        const marker = entry.excluded[0];
        if (!marker?.startsWith('\u0000shown:')) return entry;
        const shown = new Set(JSON.parse(marker.slice(7)) as string[]);
        return { ...entry, excluded: this.labels(entry).filter(l => !shown.has(l)) };
      }),
    );
  }

  detach(): void {
    this.grid = null;
    this.pictures = null;
  }

  private text(path: string): string | null {
    const part = this.preserved.parts[path];
    return part ? decoder.decode(part.bytes) : null;
  }

  private put(path: string, xml: string): void {
    this.preserved.parts[path] = { path, bytes: new Uint8Array(encoder.encode(xml)), contentType: this.preserved.parts[path]?.contentType ?? contentTypeFor(path) ?? 'application/xml' };
  }

  /** Every item a slicer offers, sorted. */
  labels(entry: SlicerEntry): string[] {
    if (entry.kind === 'pivot') {
      const pivot = this.pivots.entries().find(e => entry.pivotIds.includes(e.id));
      if (!pivot) return [];
      const live = this.pivots.itemsOf(pivot, entry.field);
      return live.length ? live : [...(this.pivots.caches.get(pivot.cachePath)?.fields.find(f => f.name === entry.field)?.items ?? [])].sort(compareLabels);
    }
    const table = this.tables.find(t => t.path === entry.tablePath);
    if (!table || !this.grid) return [];
    const offset = table.columns.findIndex(c => c.name === entry.field);
    const body = this.bodyRows(table);
    if (offset < 0 || body.endRow < body.startRow) return [];
    const column = table.range.startColumn + offset;
    const values = this.grid.values(entry.sheet, { startRow: body.startRow, endRow: body.endRow, startColumn: column, endColumn: column });
    return [...new Set(values.map(r => itemLabel(r[0] ?? null)))].sort(compareLabels);
  }

  itemsFor(entry: SlicerEntry): { label: string; selected: boolean }[] {
    const excluded = new Set(entry.excluded);
    return this.labels(entry).map(label => ({ label, selected: !excluded.has(label) }));
  }

  private bodyRows(table: TableModel): { startRow: number; endRow: number } {
    return { startRow: table.range.startRow + table.headerRows, endRow: table.range.endRow - table.totalsRows };
  }

  /** Clicking an item: on its own it selects just that item; with `extend` it toggles it. */
  click(name: string, label: string, extend: boolean): void {
    const entry = this.entries().find(e => e.name === name);
    if (!entry) return;
    const all = this.labels(entry);
    const excluded = new Set(entry.excluded);
    if (extend) {
      if (excluded.has(label)) excluded.delete(label);
      else if (excluded.size < all.length - 1) excluded.add(label);
    } else {
      excluded.clear();
      for (const l of all) if (l !== label) excluded.add(l);
    }
    this.setExcluded(name, [...excluded]);
  }

  clearFilter(name: string): void {
    this.setExcluded(name, []);
  }

  private setExcluded(name: string, excluded: string[]): void {
    this.entries.update(list => list.map(e => (e.name === name ? { ...e, excluded, dirty: true } : e)));
    const entry = this.entries().find(e => e.name === name);
    if (!entry) return;
    if (entry.kind === 'pivot') this.applyToPivots(entry);
    else this.applyToTable(entry);
    this.pictures?.update(entry, this.itemsFor(entry));
  }

  private applyToPivots(entry: SlicerEntry): void {
    for (const id of entry.pivotIds) {
      const pivot = this.pivots.entries().find(e => e.id === id);
      if (!pivot) continue;
      const hidden = { ...pivot.config.hidden };
      if (entry.excluded.length) hidden[entry.field] = entry.excluded;
      else delete hidden[entry.field];
      this.pivots.apply(id, { ...pivot.config, hidden }, { force: true });
    }
  }

  /** Rows hide when any of the table's slicers leaves them out, or a filter no slicer owns does. */
  private applyToTable(entry: SlicerEntry): void {
    const grid = this.grid;
    const table = this.tables.find(t => t.path === entry.tablePath);
    if (!grid || !table) return;
    this.tableFiltersDirty.add(table.path);
    const body = this.bodyRows(table);
    const slicers = this.entries().filter(e => e.tablePath === table.path);
    const excludedBy = new Map<number, Set<string>>();
    for (const slicer of slicers) {
      const offset = table.columns.findIndex(c => c.name === slicer.field);
      if (offset >= 0 && slicer.excluded.length) excludedBy.set(offset, new Set(slicer.excluded));
    }
    const width = table.columns.length;
    const values = grid.values(entry.sheet, { startRow: body.startRow, endRow: body.endRow, startColumn: table.range.startColumn, endColumn: table.range.startColumn + width - 1 });
    values.forEach((row, i) => {
      const hide = [...excludedBy].some(([offset, set]) => set.has(itemLabel(row[offset] ?? null)));
      const at = body.startRow + i;
      if (hide !== grid.isRowHidden(entry.sheet, at)) grid.setRowsHidden(entry.sheet, at, 1, hide);
    });
  }

  /** What a new slicer can attach to. */
  targets(): { id: string; label: string; kind: 'pivot' | 'table'; fields: string[]; sheet: string }[] {
    const pivots = this.pivots.editable().map(e => ({ id: e.id, label: `Pivot: ${e.name}`, kind: 'pivot' as const, fields: this.pivots.fieldsOf(e), sheet: e.sheet }));
    const tables = this.tables.flatMap(t => {
      const sheet = Object.entries(this.preserved.bySheet).find(([, part]) => part.tables.includes(t.path))?.[0];
      return sheet ? [{ id: t.path, label: `Table: ${t.name}`, kind: 'table' as const, fields: t.columns.map(c => c.name), sheet }] : [];
    });
    return [...pivots, ...tables];
  }

  add(targetId: string, field: string): SlicerEntry | null {
    const target = this.targets().find(t => t.id === targetId);
    if (!target || !target.fields.includes(field)) return null;
    const names = new Set(this.entries().map(e => e.name));
    let name = field;
    for (let n = 1; names.has(name); n++) name = `${field} ${n}`;
    const cacheNames = new Set([...this.caches.keys(), ...this.entries().map(e => e.cacheName)]);
    const base = `Slicer_${field.replace(/[^A-Za-z0-9_]/g, '_')}`;
    let cacheName = base;
    for (let n = 1; cacheNames.has(cacheName); n++) cacheName = `${base}${n}`;
    const anchor = this.defaultAnchor(target);
    const entry: SlicerEntry = {
      name,
      caption: field,
      cacheName,
      field,
      kind: target.kind,
      sheet: target.sheet,
      anchor,
      partPath: '',
      pivotIds: target.kind === 'pivot' ? [target.id] : [],
      tablePath: target.kind === 'table' ? target.id : null,
      excluded: [],
      isNew: true,
      dirty: true,
    };
    if (target.kind === 'pivot') {
      const pivot = this.pivots.entries().find(e => e.id === target.id);
      if (pivot?.config.hidden[field]?.length) entry.excluded = [...pivot.config.hidden[field]];
      this.pivots.useForSlicer(target.id, field);
    }
    this.entries.update(list => [...list, entry]);
    this.pictures?.add(entry, this.itemsFor(entry));
    return entry;
  }

  private defaultAnchor(target: { id: string; kind: 'pivot' | 'table'; sheet: string }): SlicerAnchor {
    let column = 0;
    let row = 0;
    if (target.kind === 'pivot') {
      const range = this.pivots.entries().find(e => e.id === target.id)?.location.range;
      if (range) {
        column = range.endColumn + 2;
        row = range.startRow;
      }
    } else {
      const range = this.tables.find(t => t.path === target.id)?.range;
      if (range) {
        column = range.endColumn + 2;
        row = range.startRow;
      }
    }
    // Clear of the charts and slicers already on the sheet.
    const taken = this.entries().filter(e => e.anchor?.from.row === row && e.anchor.from.col >= column).length;
    const start = Math.max(column + taken * 3, (this.pictures?.rightEdge?.(target.sheet) ?? -1) + 1);
    const marker = (col: number, r: number): AnchorMarker => ({ col, colOff: 0, row: r, rowOff: 0 });
    return { from: marker(start, row), to: marker(start + 3, row + 12) };
  }

  remove(name: string): void {
    const entry = this.entries().find(e => e.name === name);
    if (!entry) return;
    if (entry.excluded.length) this.setExcluded(name, []);
    this.pictures?.remove(entry);
    const book = this.preserved.slicers!;
    if (!entry.isNew) {
      this.removedNames.push(entry.cacheName);
      const xml = entry.partPath ? this.text(entry.partPath) : null;
      if (xml) {
        const rest = parseSlicerPart(xml).filter(s => s.name !== name);
        if (rest.length) this.put(entry.partPath, newSlicerPartXml(rest));
        else {
          delete this.preserved.parts[entry.partPath];
          book.sheetSlicers[entry.sheet] = (book.sheetSlicers[entry.sheet] ?? []).filter(p => p !== entry.partPath);
          if (!book.sheetSlicers[entry.sheet].length) delete book.sheetSlicers[entry.sheet];
        }
      }
      const cache = this.caches.get(entry.cacheName);
      if (cache) {
        delete this.preserved.parts[cache.path];
        book.slicerCaches = book.slicerCaches.filter(p => p !== cache.path);
        this.caches.delete(entry.cacheName);
      }
      const drawingPath = this.preserved.bySheet[entry.sheet]?.drawing?.path;
      const drawing = drawingPath ? this.text(drawingPath) : null;
      if (drawingPath && drawing) this.put(drawingPath, patchSlicerAnchor(drawing, name, null));
    }
    this.entries.update(list => list.filter(e => e.name !== name));
  }

  /** Writes changed state into the parts; rebuilds the edits the reference pass applies. */
  flush(): void {
    const book = this.preserved.slicers!;
    const anchors: Record<string, string[]> = {};
    const addNames: string[] = [];
    let shapeId = 1000;
    for (const entry of this.entries()) {
      const moved = this.pictures?.anchorOf?.(entry) ?? entry.anchor;
      if (entry.isNew) {
        this.writeNew(entry);
        addNames.push(entry.cacheName);
        if (moved) (anchors[entry.sheet] ??= []).push(slicerAnchorXml(entry.name, entry.kind, moved, shapeId++));
        continue;
      }
      const drawingPath = this.preserved.bySheet[entry.sheet]?.drawing?.path;
      if (moved && drawingPath && JSON.stringify(moved) !== JSON.stringify(entry.anchor)) {
        const drawing = this.text(drawingPath);
        if (drawing) this.put(drawingPath, patchSlicerAnchor(drawing, entry.name, moved));
        this.entries.update(list => list.map(e => (e.name === entry.name ? { ...e, anchor: moved } : e)));
      }
      if (!entry.dirty) continue;
      if (entry.kind === 'pivot') {
        const cache = this.caches.get(entry.cacheName);
        const xml = cache ? this.text(cache.path) : null;
        if (cache && xml) this.put(cache.path, updateSlicerCacheXml(xml, this.cacheItems(entry)));
      }
    }
    for (const path of this.tableFiltersDirty) this.writeTableFilters(path);
    book.edits = { addNames, removeNames: [...this.removedNames], anchors };
    this.entries.update(list => list.map(e => ({ ...e, dirty: false })));
  }

  /** Items for the slicer cache, in display order, pointing into the pivot cache's shared items. */
  private cacheItems(entry: SlicerEntry): { x: number; selected: boolean }[] {
    const pivot = this.pivots.entries().find(e => entry.pivotIds.includes(e.id));
    const field = pivot ? this.pivots.caches.get(pivot.cachePath)?.fields.find(f => f.name === entry.field) : undefined;
    const excluded = new Set(entry.excluded);
    return [...(field?.items ?? [])]
      .map((label, x) => ({ label, x }))
      .sort((a, b) => compareLabels(a.label, b.label))
      .map(({ label, x }) => ({ x, selected: !excluded.has(label) }));
  }

  private writeNew(entry: SlicerEntry): void {
    const book = this.preserved.slicers!;
    let cachePath = this.caches.get(entry.cacheName)?.path;
    if (!cachePath) {
      let n = 1;
      while (this.preserved.parts[`xl/slicerCaches/slicerCache${n}.xml`]) n++;
      cachePath = `xl/slicerCaches/slicerCache${n}.xml`;
      book.slicerCaches.push(cachePath);
    }
    if (entry.kind === 'pivot') {
      const pivots = entry.pivotIds.map(id => this.pivots.entries().find(e => e.id === id)).filter((p): p is NonNullable<typeof p> => !!p);
      const first = pivots[0];
      const cacheId = first ? this.pivots.caches.get(first.cachePath)?.pivotCacheId ?? '' : '';
      this.put(
        cachePath,
        newPivotSlicerCacheXml(entry.cacheName, entry.field, cacheId, pivots.map(p => {
          book.sheetIds[p.sheet] ??= `new-${p.sheet}`;
          return { tabId: book.sheetIds[p.sheet], name: p.name };
        }), this.cacheItems(entry)),
      );
    } else {
      const table = this.tables.find(t => t.path === entry.tablePath);
      const column = table?.columns.find(c => c.name === entry.field)?.id ?? '1';
      this.put(cachePath, newTableSlicerCacheXml(entry.cacheName, entry.field, table?.id ?? '1', column));
    }
    let partPath = entry.partPath;
    if (!partPath) {
      let n = 1;
      while (this.preserved.parts[`xl/slicers/slicer${n}.xml`]) n++;
      partPath = `xl/slicers/slicer${n}.xml`;
    }
    this.put(partPath, newSlicerPartXml([{ name: entry.name, cache: entry.cacheName, caption: entry.caption, rowHeight: DEFAULT_ROW_HEIGHT_EMU, columnCount: null, style: 'SlicerStyleLight1' }]));
    const parts = (book.sheetSlicers[entry.sheet] ??= []);
    if (!parts.includes(partPath)) parts.push(partPath);
    this.entries.update(list => list.map(e => (e.name === entry.name ? { ...e, partPath } : e)));
  }

  private writeTableFilters(path: string): void {
    const table = this.tables.find(t => t.path === path);
    const xml = this.text(path);
    if (!table || !xml) return;
    const filters: Record<number, string[]> = { ...table.filters };
    for (const entry of this.entries().filter(e => e.tablePath === path)) {
      const offset = table.columns.findIndex(c => c.name === entry.field);
      if (offset < 0) continue;
      if (entry.excluded.length) {
        const excluded = new Set(entry.excluded);
        filters[offset] = this.labels(entry).filter(l => !excluded.has(l));
      } else delete filters[offset];
    }
    this.put(path, updateTableFiltersXml(xml, filters));
    table.filters = filters;
  }
}

/** Pixels to a marker, given column and row sizes: where a dragged picture landed. */
export function pxToEmuOffset(px: number): number {
  return Math.round(px * EMU_PER_PX);
}

export { BLANK_LABEL };

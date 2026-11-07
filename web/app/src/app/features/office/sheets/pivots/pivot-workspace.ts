import { computed, signal } from '@angular/core';

import { RangeRef, parseRangeA1, rangeA1 } from '../../shared/a1';
import {
  PivotCacheModel,
  PivotLocation,
  cacheColumns,
  newPivotCacheXml,
  newPivotTableXml,
  parsePivotCache,
  parsePivotTable,
  pivotRecordsXml,
  updatePivotCacheXml,
  updatePivotTableXml,
} from '../../xlsx/xlsx-pivot-xml';
import { PreservedParts, contentTypeFor, emptyPreservedSheet } from '../../xlsx/xlsx-preserve';
import { TableModel, parseTable } from '../../xlsx/xlsx-slicers';
import { GridValue, SheetGrid } from '../sheet-grid';
import { PivotConfig, PivotSourceError, computePivot, sourceFields } from './pivot-engine';

const REL_RECORDS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheRecords';
const REL_CACHE = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/pivotCacheDefinition';
const encoder = new TextEncoder();
const decoder = new TextDecoder();

export interface PivotEntry {
  id: string;
  name: string;
  sheet: string;
  path: string;
  cachePath: string;
  location: PivotLocation;
  config: PivotConfig;
  /** Why it cannot be edited here; empty when it can. */
  limits: string[];
  /** Fields that keep an item list for a slicer. */
  itemFields: string[];
  isNew: boolean;
  dirty: boolean;
}

export type ApplyOutcome = { kind: 'ok' } | { kind: 'conflict'; where: string } | { kind: 'error'; message: string };

export function emptyConfig(): PivotConfig {
  return { rows: [], columns: [], values: [], filters: [], hidden: {}, rowGrandTotals: true, colGrandTotals: true };
}

/**
 * The open workbook's pivot tables. Edits land on the sheet at once through SheetGrid; the file's
 * parts are rewritten only in flush, for the pivots that changed, so an untouched pivot goes back byte
 * for byte. A plain class the sheets editor owns: one per open file.
 */
export class PivotWorkspace {
  readonly entries = signal<readonly PivotEntry[]>([]);
  /** A source changed since its pivot last computed: offer a refresh. */
  readonly sourceChanged = signal(false);
  readonly editable = computed(() => this.entries().filter(e => !e.limits.length));
  readonly caches = new Map<string, PivotCacheModel>();
  /** Cache ids by path. */
  private readonly cacheIds = new Map<string, string>();
  /** Ids a new slicer asked for, to write on the cache. */
  readonly pendingPivotCacheIds = new Map<string, string>();
  private fingerprints = new Map<string, string>();
  private grid: SheetGrid | null = null;
  private stopListening: (() => void) | null = null;
  private nextNew = 1;
  private readonly tables: TableModel[] = [];

  constructor(private readonly preserved: PreservedParts) {
    for (const entry of Object.values(preserved.bySheet)) {
      for (const path of entry.tables) {
        const xml = this.text(path);
        if (xml) this.tables.push(parseTable(xml, path));
      }
    }
    for (const { cacheId, path } of preserved.pivotCaches) {
      const xml = this.text(path);
      if (!xml) continue;
      this.caches.set(path, parsePivotCache(xml, path));
      this.cacheIds.set(path, cacheId);
    }
    const entries: PivotEntry[] = [];
    for (const [sheet, part] of Object.entries(preserved.bySheet)) {
      for (const path of part.pivotTables) {
        const xml = this.text(path);
        const cachePath = this.cachePathOf(path);
        const cache = cachePath ? this.caches.get(cachePath) : undefined;
        if (!xml || !cache || !cachePath) continue;
        const model = parsePivotTable(xml, path, cache);
        entries.push({
          id: path,
          name: model.name,
          sheet,
          path,
          cachePath,
          location: model.location,
          config: model.config,
          limits: model.limits,
          itemFields: model.itemFields,
          isNew: false,
          dirty: false,
        });
      }
    }
    this.entries.set(entries);
  }

  /** Called once the sheet is open. */
  attach(grid: SheetGrid): void {
    this.detach();
    this.grid = grid;
    for (const cachePath of this.caches.keys()) this.remember(cachePath);
    this.stopListening = grid.onChange(() => this.checkSources());
  }

  detach(): void {
    this.stopListening?.();
    this.stopListening = null;
    this.grid = null;
  }

  private text(path: string): string | null {
    const part = this.preserved.parts[path];
    return part ? decoder.decode(part.bytes) : null;
  }

  private put(path: string, xml: string): void {
    this.preserved.parts[path] = { path, bytes: new Uint8Array(encoder.encode(xml)), contentType: this.preserved.parts[path]?.contentType ?? contentTypeFor(path) ?? 'application/xml' };
  }

  private cachePathOf(pivotPath: string): string | null {
    const rel = (this.preserved.rels[pivotPath] ?? []).find(r => r.type === REL_CACHE);
    if (!rel) return null;
    const base = pivotPath.slice(0, pivotPath.lastIndexOf('/') + 1);
    const out: string[] = [];
    for (const segment of (base + rel.target).split('/')) {
      if (segment === '..') out.pop();
      else if (segment !== '.' && segment) out.push(segment);
    }
    return rel.target.startsWith('/') ? rel.target.slice(1) : out.join('/');
  }

  /** Where the cache reads from, resolving a table name. */
  sourceOf(cachePath: string): { sheet: string; range: RangeRef } | null {
    const source = this.caches.get(cachePath)?.source;
    if (source?.kind === 'range') return { sheet: source.sheet, range: source.range };
    if (source?.kind === 'table') {
      const table = this.tables.find(t => t.name === source.name);
      if (!table) return null;
      const sheet = Object.entries(this.preserved.bySheet).find(([, part]) => part.tables.includes(table.path))?.[0];
      if (!sheet) return null;
      return { sheet, range: { ...table.range, endRow: table.range.endRow - table.totalsRows } };
    }
    return null;
  }

  sourceTable(cachePath: string): GridValue[][] | null {
    const source = this.sourceOf(cachePath);
    return source && this.grid ? this.grid.values(source.sheet, source.range) : null;
  }

  /** The source's field names for the panel, or the reason it has none. */
  fieldsOf(entry: PivotEntry): string[] {
    const table = this.sourceTable(entry.cachePath);
    if (!table) return this.caches.get(entry.cachePath)?.fields.map(f => f.name) ?? [];
    try {
      return sourceFields(table);
    } catch {
      return this.caches.get(entry.cachePath)?.fields.map(f => f.name) ?? [];
    }
  }

  /** Every label a field has in the source, sorted. */
  itemsOf(entry: PivotEntry, field: string): string[] {
    const table = this.sourceTable(entry.cachePath);
    if (!table) return [];
    try {
      return computePivot({ ...emptyConfig() }, table).fieldItems[field] ?? [];
    } catch {
      return [];
    }
  }

  private rects(entry: Pick<PivotEntry, 'location'>): RangeRef[] {
    const loc = entry.location;
    const out = [loc.range];
    if (loc.pageRows) {
      out.push({
        startRow: Math.max(0, loc.row - loc.pageRows - 1),
        startColumn: loc.column,
        endRow: Math.max(0, loc.row - 2),
        endColumn: loc.column + 1,
      });
    }
    return out;
  }

  /** Lays the pivot out on its sheet. Refuses to write over other cells unless forced. */
  apply(id: string, config: PivotConfig, opts: { force?: boolean } = {}): ApplyOutcome {
    const grid = this.grid;
    const entry = this.entries().find(e => e.id === id);
    if (!grid || !entry) return { kind: 'error', message: 'The sheet is not ready.' };
    if (entry.limits.length) return { kind: 'error', message: `This pivot table can't be changed here because ${entry.limits[0]}.` };
    const table = this.sourceTable(entry.cachePath);
    if (!table) return { kind: 'error', message: 'The source of this pivot table could not be read.' };
    let result;
    try {
      result = computePivot(config, table);
    } catch (error) {
      if (error instanceof PivotSourceError) return { kind: 'error', message: error.message };
      throw error;
    }
    const pageRows = result.pageCells.length;
    const row = Math.max(entry.location.row, pageRows ? pageRows + 1 : 0);
    const column = entry.location.column;
    const body: RangeRef = { startRow: row, startColumn: column, endRow: row + result.cells.length - 1, endColumn: column + result.cells[0].length - 1 };
    const next: PivotLocation = { row, column, range: body, pageRows };
    const source = this.sourceOf(entry.cachePath);
    const newRects = this.rects({ location: next });
    if (source && source.sheet === entry.sheet && newRects.some(r => overlaps(r, source.range))) {
      return { kind: 'error', message: 'A pivot table cannot be placed over its own source.' };
    }
    const oldRects = entry.isNew && !entry.dirty ? [] : this.rects(entry);
    if (!opts.force) {
      const taken = this.firstTaken(entry.sheet, newRects, oldRects);
      if (taken) return { kind: 'conflict', where: taken };
    }
    for (const r of oldRects) grid.clear(entry.sheet, r);
    grid.write(entry.sheet, row, column, result.cells);
    if (pageRows) grid.write(entry.sheet, row - pageRows - 1, column, result.pageCells);
    this.update(id, { config, location: next, dirty: true });
    this.remember(entry.cachePath);
    this.checkSources();
    return { kind: 'ok' };
  }

  /** The first filled cell a layout would overwrite that is not the pivot's own. */
  private firstTaken(sheet: string, target: readonly RangeRef[], own: readonly RangeRef[]): string | null {
    for (const r of target) {
      const values = this.grid?.values(sheet, r) ?? [];
      for (let y = 0; y < values.length; y++) {
        for (let x = 0; x < values[y].length; x++) {
          const v = values[y][x];
          const cellRow = r.startRow + y;
          const cellCol = r.startColumn + x;
          if (v === null || v === '') continue;
          if (own.some(o => cellRow >= o.startRow && cellRow <= o.endRow && cellCol >= o.startColumn && cellCol <= o.endColumn)) continue;
          return rangeA1({ startRow: cellRow, startColumn: cellCol, endRow: cellRow, endColumn: cellCol });
        }
      }
    }
    return null;
  }

  refresh(id: string): ApplyOutcome {
    const entry = this.entries().find(e => e.id === id);
    return entry ? this.apply(id, entry.config, { force: true }) : { kind: 'error', message: 'No such pivot table.' };
  }

  refreshAll(): void {
    for (const entry of this.editable()) this.refresh(entry.id);
    this.sourceChanged.set(false);
  }

  /** A new pivot over a range with a header row, at a cell. It shows nothing until fields are chosen. */
  create(source: { sheet: string; range: RangeRef }, target: { sheet: string; row: number; column: number }): { entry: PivotEntry } | { error: string } {
    const grid = this.grid;
    if (!grid) return { error: 'The sheet is not ready.' };
    try {
      sourceFields(grid.values(source.sheet, source.range));
    } catch (error) {
      if (error instanceof PivotSourceError) return { error: error.message };
      throw error;
    }
    const taken = new Set(this.entries().map(e => e.name));
    let n = this.nextNew;
    while (taken.has(`PivotTable${n}`)) n++;
    this.nextNew = n + 1;
    const pivotPath = this.freePath('xl/pivotTables/pivotTable', '.xml');
    const cachePath = this.freePath('xl/pivotCache/pivotCacheDefinition', '.xml');
    const cacheId = String(Math.max(0, ...this.preserved.pivotCaches.map(c => Number(c.cacheId) || 0)) + 1);
    this.put(cachePath, newPivotCacheXml(source.sheet, rangeA1(source.range)));
    this.put(pivotPath, newPivotTableXml(`PivotTable${n}`, cacheId));
    this.preserved.rels[pivotPath] = [{ id: 'rId1', type: REL_CACHE, target: `../pivotCache/${cachePath.split('/').pop()}`, external: false }];
    this.preserved.pivotCaches.push({ cacheId, path: cachePath });
    (this.preserved.bySheet[target.sheet] ??= emptyPreservedSheet()).pivotTables.push(pivotPath);
    this.caches.set(cachePath, { path: cachePath, source: { kind: 'range', sheet: source.sheet, range: source.range }, fields: [], pivotCacheId: null });
    this.cacheIds.set(cachePath, cacheId);
    const entry: PivotEntry = {
      id: pivotPath,
      name: `PivotTable${n}`,
      sheet: target.sheet,
      path: pivotPath,
      cachePath,
      location: { row: target.row, column: target.column, range: { startRow: target.row, startColumn: target.column, endRow: target.row, endColumn: target.column }, pageRows: 0 },
      config: emptyConfig(),
      limits: [],
      itemFields: [],
      isNew: true,
      dirty: false,
    };
    this.entries.update(list => [...list, entry]);
    this.remember(cachePath);
    return { entry };
  }

  /** Clears the cells and takes the pivot, and a cache nothing else uses, out of the file. */
  remove(id: string): void {
    const entry = this.entries().find(e => e.id === id);
    if (!entry) return;
    if (entry.dirty || !entry.isNew) for (const r of this.rects(entry)) this.grid?.clear(entry.sheet, r);
    const part = this.preserved.bySheet[entry.sheet];
    if (part) part.pivotTables = part.pivotTables.filter(p => p !== entry.path);
    delete this.preserved.parts[entry.path];
    delete this.preserved.rels[entry.path];
    this.entries.update(list => list.filter(e => e.id !== id));
    if (!this.entries().some(e => e.cachePath === entry.cachePath)) {
      const records = this.recordsPath(entry.cachePath);
      if (records) delete this.preserved.parts[records];
      delete this.preserved.parts[entry.cachePath];
      delete this.preserved.rels[entry.cachePath];
      this.preserved.pivotCaches = this.preserved.pivotCaches.filter(c => c.path !== entry.cachePath);
      this.caches.delete(entry.cachePath);
    }
  }

  /** Marks a pivot to keep an item list for a slicer, and its cache to carry an id the slicer can name. */
  useForSlicer(id: string, field: string): string {
    const entry = this.entries().find(e => e.id === id);
    if (!entry) return '';
    if (!entry.itemFields.includes(field)) this.update(id, { itemFields: [...entry.itemFields, field], dirty: true });
    const cache = this.caches.get(entry.cachePath);
    if (cache?.pivotCacheId) return cache.pivotCacheId;
    const ids = [...this.caches.values()].map(c => Number(c.pivotCacheId) || 0);
    const fresh = String(Math.max(0, ...ids, ...[...this.pendingPivotCacheIds.values()].map(Number)) + 1);
    const existing = this.pendingPivotCacheIds.get(entry.cachePath);
    if (existing) return existing;
    this.pendingPivotCacheIds.set(entry.cachePath, fresh);
    return fresh;
  }

  private recordsPath(cachePath: string): string | null {
    const rel = (this.preserved.rels[cachePath] ?? []).find(r => r.type === REL_RECORDS);
    return rel ? `${cachePath.slice(0, cachePath.lastIndexOf('/') + 1)}${rel.target}` : null;
  }

  private freePath(prefix: string, suffix: string): string {
    let n = 1;
    while (this.preserved.parts[`${prefix}${n}${suffix}`]) n++;
    return `${prefix}${n}${suffix}`;
  }

  private update(id: string, patch: Partial<PivotEntry>): void {
    this.entries.update(list => list.map(e => (e.id === id ? { ...e, ...patch } : e)));
  }

  private remember(cachePath: string): void {
    this.fingerprints.set(cachePath, JSON.stringify(this.sourceTable(cachePath)));
  }

  private checkSources(): void {
    for (const cachePath of this.caches.keys()) {
      if (this.fingerprints.get(cachePath) !== JSON.stringify(this.sourceTable(cachePath))) {
        this.sourceChanged.set(true);
        return;
      }
    }
    this.sourceChanged.set(false);
  }

  /** Writes the changed pivots and their caches into the preserved parts, for the save. */
  flush(): void {
    const dirtyCaches = new Set(this.entries().filter(e => e.dirty).map(e => e.cachePath));
    for (const path of this.pendingPivotCacheIds.keys()) dirtyCaches.add(path);
    for (const cachePath of dirtyCaches) {
      const table = this.sourceTable(cachePath);
      const cache = this.caches.get(cachePath);
      const xml = this.text(cachePath);
      if (!table || !cache || !xml) continue;
      const siblings = this.entries().filter(e => e.cachePath === cachePath);
      const axis = new Set(siblings.flatMap(e => [...e.config.rows, ...e.config.columns, ...e.config.filters, ...e.itemFields, ...Object.keys(e.config.hidden)]));
      const columns = cacheColumns(table, cache.fields, axis);
      const source = this.sourceOf(cachePath);
      const pivotCacheId = this.pendingPivotCacheIds.get(cachePath);
      let updated = updatePivotCacheXml(xml, columns, table.length - 1, {
        previous: cache.fields,
        ...(source && cache.source.kind === 'range' ? { ref: rangeA1(source.range), sheet: source.sheet } : {}),
        ...(pivotCacheId ? { pivotCacheId } : {}),
      });
      let records = this.recordsPath(cachePath);
      if (!records) {
        records = this.freePath('xl/pivotCache/pivotCacheRecords', '.xml');
        const rels = this.preserved.rels[cachePath] ?? [];
        const relId = rels.some(r => r.id === 'rId1') ? `rId${rels.length + 1}` : 'rId1';
        this.preserved.rels[cachePath] = [...rels, { id: relId, type: REL_RECORDS, target: records.split('/').pop()!, external: false }];
        updated = updated.replace(/\sr:id="[^"]*"/, '').replace(/<pivotCacheDefinition\b/, `<pivotCacheDefinition r:id="${relId}"`);
        if (!/xmlns:r=/.test(updated)) updated = updated.replace(/<pivotCacheDefinition\b/, '<pivotCacheDefinition xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"');
      }
      this.put(cachePath, updated);
      this.put(records, pivotRecordsXml(table, columns));
      this.caches.set(cachePath, parsePivotCache(updated, cachePath));
      for (const sibling of siblings) {
        const pivotXml = this.text(sibling.path);
        if (!pivotXml) continue;
        const result = computePivot(sibling.config, table);
        this.put(sibling.path, updatePivotTableXml(pivotXml, { config: sibling.config, result, columns, ref: rangeA1(sibling.location.range), itemFields: sibling.itemFields }));
      }
    }
    this.pendingPivotCacheIds.clear();
    this.entries.update(list => list.map(e => (dirtyCaches.has(e.cachePath) ? { ...e, dirty: false, isNew: false } : e)));
  }
}

function overlaps(a: RangeRef, b: RangeRef): boolean {
  return a.startRow <= b.endRow && b.startRow <= a.endRow && a.startColumn <= b.endColumn && b.startColumn <= a.endColumn;
}

/** A1 text to a cell, for the panel. */
export function parseCell(text: string): { row: number; column: number } | null {
  const range = parseRangeA1(text.trim().toUpperCase());
  return range ? { row: range.startRow, column: range.startColumn } : null;
}

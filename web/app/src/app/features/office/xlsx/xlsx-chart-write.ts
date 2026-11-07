import { NS, child, children, elementChildren, parseXml, serializeXml } from '../shared/ooxml';
import { C, ChartGrouping, ChartKind, ChartSpec, LiveSeries, LegendPosition, sameAsCached } from '../sheets/charts/chart-spec';

/**
 * Writes charts back. A chart part keeps everything we do not model: an edit changes only the elements
 * it must, in schema order, and a chart nobody touched whose data did not change returns the same text.
 */

const A = NS.a;
const R = NS.r;
const XML_DECLARATION = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n';

/** A chart's kinds as schema: its group element, and its children and its series' children in order. */
const GROUP_NAME: Record<ChartKind, string> = {
  column: 'barChart',
  bar: 'barChart',
  line: 'lineChart',
  area: 'areaChart',
  pie: 'pieChart',
  doughnut: 'doughnutChart',
  scatter: 'scatterChart',
  radar: 'radarChart',
};
const GROUP_ORDER: Record<string, readonly string[]> = {
  barChart: ['barDir', 'grouping', 'varyColors', 'ser', 'dLbls', 'gapWidth', 'overlap', 'serLines', 'axId', 'extLst'],
  lineChart: ['grouping', 'varyColors', 'ser', 'dLbls', 'dropLines', 'hiLowLines', 'upDownBars', 'marker', 'smooth', 'axId', 'extLst'],
  areaChart: ['grouping', 'varyColors', 'ser', 'dLbls', 'dropLines', 'axId', 'extLst'],
  pieChart: ['varyColors', 'ser', 'dLbls', 'firstSliceAng', 'extLst'],
  doughnutChart: ['varyColors', 'ser', 'dLbls', 'firstSliceAng', 'holeSize', 'extLst'],
  scatterChart: ['scatterStyle', 'varyColors', 'ser', 'dLbls', 'axId', 'extLst'],
  radarChart: ['radarStyle', 'varyColors', 'ser', 'dLbls', 'axId', 'extLst'],
};
const SER_ORDER: Record<string, readonly string[]> = {
  barChart: ['idx', 'order', 'tx', 'spPr', 'invertIfNegative', 'pictureOptions', 'dPt', 'dLbls', 'trendline', 'errBars', 'cat', 'val', 'shape', 'extLst'],
  lineChart: ['idx', 'order', 'tx', 'spPr', 'marker', 'dPt', 'dLbls', 'trendline', 'errBars', 'cat', 'val', 'smooth', 'extLst'],
  areaChart: ['idx', 'order', 'tx', 'spPr', 'pictureOptions', 'dPt', 'dLbls', 'trendline', 'errBars', 'cat', 'val', 'extLst'],
  pieChart: ['idx', 'order', 'tx', 'spPr', 'explosion', 'dPt', 'dLbls', 'cat', 'val', 'extLst'],
  doughnutChart: ['idx', 'order', 'tx', 'spPr', 'explosion', 'dPt', 'dLbls', 'cat', 'val', 'extLst'],
  scatterChart: ['idx', 'order', 'tx', 'spPr', 'marker', 'dPt', 'dLbls', 'trendline', 'errBars', 'xVal', 'yVal', 'smooth', 'extLst'],
  radarChart: ['idx', 'order', 'tx', 'spPr', 'marker', 'dPt', 'dLbls', 'cat', 'val', 'extLst'],
};
const NO_AXES = new Set(['pieChart', 'doughnutChart']);

function el(doc: Document, name: string, val?: string): Element {
  const out = doc.createElementNS(C, `c:${name}`);
  if (val !== undefined) out.setAttribute('val', val);
  return out;
}

function sortChildren(parent: Element, order: readonly string[]): void {
  const rank = (e: Element) => {
    const i = order.indexOf(e.localName);
    return i === -1 ? order.length : i;
  };
  const sorted = elementChildren(parent)
    .map((e, i) => ({ e, i }))
    .sort((a, b) => rank(a.e) - rank(b.e) || a.i - b.i);
  for (const { e } of sorted) parent.appendChild(e);
}

function setChild(parent: Element, name: string, val: string): void {
  const existing = child(parent, C, name);
  if (existing) existing.setAttribute('val', val);
  else parent.appendChild(el(parent.ownerDocument, name, val));
}

function removeChild(parent: Element, name: string): void {
  child(parent, C, name)?.remove();
}

// --- references and caches ---------------------------------------------------------------------

function writePoints(cache: Element, points: readonly (string | number | null)[]): void {
  const doc = cache.ownerDocument;
  for (const old of [...children(cache, C, 'ptCount'), ...children(cache, C, 'pt')]) old.remove();
  const extLst = child(cache, C, 'extLst');
  const add = (node: Element) => cache.insertBefore(node, extLst);
  add(el(doc, 'ptCount', String(points.length)));
  points.forEach((point, i) => {
    if (point === null || point === '') return;
    const pt = el(doc, 'pt');
    pt.setAttribute('idx', String(i));
    const v = el(doc, 'v');
    v.textContent = String(point);
    pt.appendChild(v);
    add(pt);
  });
}

/** Points the holder (`c:tx`, `c:cat`, `c:val`…) at a formula with these values, keeping a number format it has. */
function writeRef(holder: Element, formula: string, points: readonly (string | number | null)[], numeric: boolean): void {
  const doc = holder.ownerDocument;
  const tag = numeric ? 'numRef' : 'strRef';
  const cacheTag = numeric ? 'numCache' : 'strCache';
  const existing = child(holder, C, tag);
  const format = child(child(child(holder, C, 'numRef'), C, 'numCache'), C, 'formatCode')?.textContent ?? 'General';
  for (const e of elementChildren(holder)) if (e !== existing) e.remove();
  const ref = existing ?? holder.appendChild(el(doc, tag));
  let f = child(ref, C, 'f');
  if (!f) f = ref.insertBefore(el(doc, 'f'), ref.firstChild);
  f.textContent = formula;
  let cache = child(ref, C, cacheTag);
  if (!cache) {
    cache = el(doc, cacheTag);
    ref.insertBefore(cache, child(ref, C, 'extLst'));
    if (numeric) {
      const code = el(doc, 'formatCode');
      code.textContent = format;
      cache.appendChild(code);
    }
  }
  writePoints(cache, points);
}

function firstRefName(holder: Element | null): 'numRef' | 'strRef' | 'multiLvlStrRef' | null {
  if (child(holder, C, 'numRef')) return 'numRef';
  if (child(holder, C, 'strRef')) return 'strRef';
  return child(holder, C, 'multiLvlStrRef') ? 'multiLvlStrRef' : null;
}

function seriesElements(group: Element): Element[] {
  return children(group, C, 'ser');
}

function firstGroup(doc: Document): Element {
  const plotArea = doc.getElementsByTagNameNS(C, 'plotArea')[0];
  const group = elementChildren(plotArea).find(e => e.namespaceURI === C && /Chart$/.test(e.localName));
  if (!group) throw new Error('A chart part with no chart group');
  return group;
}

/** Brings every readable reference's cache up to date. */
function refreshCaches(group: Element, spec: ChartSpec, live: readonly LiveSeries[]): void {
  const scatter = spec.kind === 'scatter';
  seriesElements(group).forEach((ser, i) => {
    const series = spec.series[i];
    const now = live[i];
    if (!series || !now) return;
    if (series.nameRef?.cells) writeRef(ensureHolder(ser, 'tx', group), series.nameRef.formula, [now.name], false);
    if (series.categories?.cells) {
      const numeric = now.numericLabels || (scatter && now.labels.every(l => l !== '' && Number.isFinite(Number(l))));
      writeRef(ensureHolder(ser, scatter ? 'xVal' : 'cat', group), series.categories.formula, now.labels, numeric);
    }
    if (series.values?.cells) writeRef(ensureHolder(ser, scatter ? 'yVal' : 'val', group), series.values.formula, now.values, true);
  });
}

function ensureHolder(ser: Element, name: string, group: Element): Element {
  let holder = child(ser, C, name);
  if (!holder) {
    holder = ser.appendChild(el(ser.ownerDocument, name));
    sortChildren(ser, SER_ORDER[group.localName]);
  }
  return holder;
}

// --- editing -----------------------------------------------------------------------------------

/** What the panel can change, for telling whether anything did. */
function editFingerprint(spec: ChartSpec): string {
  return JSON.stringify([
    spec.kind,
    spec.grouping,
    spec.title,
    spec.legend,
    spec.categoryAxisTitle,
    spec.valueAxisTitle,
    spec.series.map(s => [s.sourceIndex, s.nameLiteral, s.nameRef?.formula ?? null, s.categories?.formula ?? null, s.values?.formula ?? null, s.color]),
  ]);
}

/** The text of a chart part for saving. */
export function chartXmlForSave(originalXml: string, before: ChartSpec, after: ChartSpec, live: readonly LiveSeries[]): string {
  if (!after.editable) return originalXml;
  const edited = editFingerprint(before) !== editFingerprint(after);
  if (!edited && sameAsCached(before, live)) return originalXml;
  const doc = parseXml(originalXml);
  if (edited) applyEdits(doc, before, after);
  refreshCaches(firstGroup(doc), after, live);
  return XML_DECLARATION + serializeXml(doc.documentElement);
}

/** A complete chart part for a chart made here. */
export function newChartXml(spec: ChartSpec, live: readonly LiveSeries[]): string {
  const doc = parseXml(
    `<c:chartSpace xmlns:c="${C}" xmlns:a="${A}" xmlns:r="${R}"><c:roundedCorners val="0"/><c:chart><c:autoTitleDeleted val="1"/>` +
      `<c:plotArea><c:layout/><c:barChart><c:barDir val="col"/><c:grouping val="clustered"/><c:varyColors val="0"/><c:gapWidth val="150"/></c:barChart></c:plotArea>` +
      `<c:plotVisOnly val="1"/><c:dispBlanksAs val="gap"/></c:chart></c:chartSpace>`,
  );
  const blank: ChartSpec = { ...spec, kind: 'column', grouping: 'standard', title: '', legend: 'none', categoryAxisTitle: '', valueAxisTitle: '', series: [] };
  applyEdits(doc, blank, spec);
  refreshCaches(firstGroup(doc), spec, live);
  return XML_DECLARATION + serializeXml(doc.documentElement);
}

function applyEdits(doc: Document, before: ChartSpec, after: ChartSpec): void {
  const chart = doc.getElementsByTagNameNS(C, 'chart')[0];
  const plotArea = child(chart, C, 'plotArea') as Element;
  let group = firstGroup(doc);
  const groupName = GROUP_NAME[after.kind];
  rebuildSeries(group, after);
  if (group.localName !== groupName) group = retype(group, groupName);
  applyGrouping(group, after.kind, after.grouping);
  ensureAxes(plotArea, group, after.kind);
  after.series.forEach((series, i) => {
    const ser = seriesElements(group)[i];
    const was = series.sourceIndex === null ? null : (before.series.find(s => s.sourceIndex === series.sourceIndex) ?? null);
    writeSeries(ser, series, was, group, after.kind);
  });
  if (after.title !== before.title) setChartTitle(chart, after.title);
  if (after.legend !== before.legend) setLegend(chart, plotArea, after.legend);
  const axes = elementChildren(plotArea).filter(e => e.namespaceURI === C && /Ax$/.test(e.localName));
  const categoryAxis = axes.find(a => a.localName === 'catAx' || a.localName === 'dateAx') ?? axes.find(a => a.localName === 'valAx');
  const valueAxis = axes.find(a => a.localName === 'valAx' && a !== categoryAxis);
  if (categoryAxis && after.categoryAxisTitle !== before.categoryAxisTitle) setAxisTitle(categoryAxis, after.categoryAxisTitle);
  if (valueAxis && after.valueAxisTitle !== before.valueAxisTitle) setAxisTitle(valueAxis, after.valueAxisTitle);
}

/** Series follow the spec's order: kept ones by their original place, new ones added, the rest removed. */
function rebuildSeries(group: Element, spec: ChartSpec): void {
  const doc = group.ownerDocument;
  const original = seriesElements(group);
  let nextIdx = Math.max(-1, ...original.map(s => Number(child(s, C, 'idx')?.getAttribute('val') ?? -1))) + 1;
  const kept: Element[] = spec.series.map(series => {
    const found = series.sourceIndex === null ? undefined : original[series.sourceIndex];
    if (found) return found;
    const ser = el(doc, 'ser');
    ser.appendChild(el(doc, 'idx', String(nextIdx++)));
    ser.appendChild(el(doc, 'order', '0'));
    return ser;
  });
  for (const ser of original) ser.remove();
  kept.forEach((ser, i) => {
    setChild(ser, 'order', String(i));
    group.appendChild(ser);
  });
  sortChildren(group, GROUP_ORDER[group.localName]);
}

function writeSeries(ser: Element, series: ChartSpec['series'][number], was: ChartSpec['series'][number] | null, group: Element, kind: ChartKind): void {
  const scatter = kind === 'scatter';
  if (series.nameRef) {
    // Its text and cache are written with the others in refreshCaches, or kept if it cannot be read.
    if (series.nameRef.cells) ensureHolder(ser, 'tx', group);
  } else if (series.nameLiteral !== null && series.nameLiteral !== was?.nameLiteral) {
    const tx = ensureHolder(ser, 'tx', group);
    for (const e of elementChildren(tx)) e.remove();
    const v = el(ser.ownerDocument, 'v');
    v.textContent = series.nameLiteral;
    tx.appendChild(v);
  }
  // A reference that was dropped goes too; one we could not read and did not change is left alone.
  const holder = (name: string, ref: typeof series.values, previous: typeof series.values) => {
    if (ref?.formula === previous?.formula && ref) return;
    if (!ref) {
      child(ser, C, name)?.remove();
      return;
    }
    const target = ensureHolder(ser, name, group);
    if (firstRefName(target) === 'multiLvlStrRef') for (const e of elementChildren(target)) e.remove();
  };
  holder(scatter ? 'xVal' : 'cat', series.categories, was?.categories ?? null);
  holder(scatter ? 'yVal' : 'val', series.values, was?.values ?? null);
  if (series.color !== (was?.color ?? null) && series.color) setSeriesColor(ser, series.color, group, kind);
  if ((kind === 'line' || kind === 'scatter') && !child(ser, C, 'smooth')) {
    // Excel reads a missing element as smoothed.
    ser.appendChild(el(ser.ownerDocument, 'smooth', '0'));
    sortChildren(ser, SER_ORDER[group.localName]);
  }
}

const FILLS = ['noFill', 'solidFill', 'gradFill', 'blipFill', 'pattFill', 'grpFill'];

function setSeriesColor(ser: Element, color: string, group: Element, kind: ChartKind): void {
  const doc = ser.ownerDocument;
  const a = (name: string) => doc.createElementNS(A, `a:${name}`);
  let spPr = child(ser, C, 'spPr');
  if (!spPr) {
    spPr = ser.appendChild(el(doc, 'spPr'));
    sortChildren(ser, SER_ORDER[group.localName]);
  }
  const solid = a('solidFill');
  const rgb = a('srgbClr');
  rgb.setAttribute('val', color.replace('#', '').toUpperCase());
  solid.appendChild(rgb);
  const fillHolder = kind === 'line' || kind === 'scatter' || kind === 'radar' ? (child(spPr, A, 'ln') ?? spPr.appendChild(a('ln'))) : spPr;
  for (const e of elementChildren(fillHolder)) if (e.namespaceURI === A && FILLS.includes(e.localName)) e.remove();
  if (fillHolder === spPr) {
    // A shape's fill follows its geometry and comes before its outline.
    const after = elementChildren(spPr).find(e => !['xfrm', 'custGeom', 'prstGeom'].includes(e.localName)) ?? null;
    spPr.insertBefore(solid, after);
  } else {
    fillHolder.insertBefore(solid, fillHolder.firstChild);
  }
}

/** The same series under another chart group, with only the children its schema allows. */
function retype(group: Element, groupName: string): Element {
  const doc = group.ownerDocument;
  const next = el(doc, groupName);
  const scatter = groupName === 'scatterChart';
  for (const e of [...elementChildren(group)]) {
    if (e.localName === 'ser') {
      for (const c of elementChildren(e)) {
        const rename: Record<string, string> = scatter ? { cat: 'xVal', val: 'yVal' } : { xVal: 'cat', yVal: 'val' };
        const to = rename[c.localName];
        if (!to) continue;
        const moved = el(doc, to);
        while (c.firstChild) moved.appendChild(c.firstChild);
        e.replaceChild(moved, c);
      }
      for (const c of elementChildren(e)) if (!SER_ORDER[groupName].includes(c.localName)) c.remove();
      sortChildren(e, SER_ORDER[groupName]);
      next.appendChild(e);
    } else if (e.localName === 'dLbls' || e.localName === 'extLst' || (e.localName === 'axId' && !NO_AXES.has(groupName))) {
      next.appendChild(e);
    }
  }
  group.parentNode?.replaceChild(next, group);
  return next;
}

function applyGrouping(group: Element, kind: ChartKind, grouping: ChartGrouping): void {
  const name = group.localName;
  const round = NO_AXES.has(name);
  setChild(group, 'varyColors', round ? '1' : '0');
  if (name === 'barChart') {
    setChild(group, 'barDir', kind === 'bar' ? 'bar' : 'col');
    setChild(group, 'grouping', grouping === 'standard' ? 'clustered' : grouping);
    if (!child(group, C, 'gapWidth')) setChild(group, 'gapWidth', '150');
    if (grouping === 'standard') removeChild(group, 'overlap');
    else setChild(group, 'overlap', '100');
  } else if (name === 'lineChart' || name === 'areaChart') {
    setChild(group, 'grouping', grouping);
    if (name === 'lineChart' && !child(group, C, 'marker')) setChild(group, 'marker', '1');
  } else if (name === 'scatterChart') {
    if (!child(group, C, 'scatterStyle')) setChild(group, 'scatterStyle', 'lineMarker');
  } else if (name === 'radarChart') {
    if (!child(group, C, 'radarStyle')) setChild(group, 'radarStyle', 'marker');
  } else {
    if (!child(group, C, 'firstSliceAng')) setChild(group, 'firstSliceAng', '0');
    if (name === 'doughnutChart' && !child(group, C, 'holeSize')) setChild(group, 'holeSize', '50');
  }
  sortChildren(group, GROUP_ORDER[name]);
}

/** Round charts have no axes, scatter has two value axes, the rest a category and a value axis. */
function ensureAxes(plotArea: Element, group: Element, kind: ChartKind): void {
  const doc = plotArea.ownerDocument;
  const axes = elementChildren(plotArea).filter(e => e.namespaceURI === C && /Ax$/.test(e.localName));
  const remove = () => axes.forEach(a => a.remove());
  if (NO_AXES.has(group.localName)) {
    remove();
    for (const id of children(group, C, 'axId')) id.remove();
    return;
  }
  const horizontal = kind === 'bar';
  const scatter = kind === 'scatter';
  const fits = scatter
    ? axes.length === 2 && axes.every(a => a.localName === 'valAx')
    : axes.length >= 2 && axes.some(a => a.localName === 'catAx' || a.localName === 'dateAx') && axes.some(a => a.localName === 'valAx');
  if (!fits) {
    remove();
    for (const id of children(group, C, 'axId')) id.remove();
    const ids = ['111111111', '222222222'];
    for (const id of ids) group.appendChild(el(doc, 'axId', id));
    sortChildren(group, GROUP_ORDER[group.localName]);
    const at = group.nextSibling;
    const fragment = parseXml(
      `<r xmlns:c="${C}">${scatter ? axisXml('valAx', ids[0], ids[1], 'b', false, 'midCat') : axisXml('catAx', ids[0], ids[1], 'b', false)}${axisXml('valAx', ids[1], ids[0], 'l', true, scatter ? 'midCat' : 'between')}</r>`,
    );
    for (const axis of elementChildren(fragment.documentElement)) plotArea.insertBefore(doc.importNode(axis, true), at);
  }
  // Which way the axes lie follows the bars.
  for (const axis of elementChildren(plotArea).filter(e => e.namespaceURI === C && /Ax$/.test(e.localName))) {
    const isValue = axis.localName === 'valAx';
    if (scatter) continue;
    child(axis, C, 'axPos')?.setAttribute('val', horizontal ? (isValue ? 'b' : 'l') : isValue ? 'l' : 'b');
  }
}

function axisXml(kind: 'catAx' | 'valAx', id: string, crossId: string, pos: string, grid: boolean, crossBetween?: string): string {
  const common =
    `<c:axId val="${id}"/><c:scaling><c:orientation val="minMax"/></c:scaling><c:delete val="0"/><c:axPos val="${pos}"/>` +
    (grid ? '<c:majorGridlines/>' : '') +
    `<c:numFmt formatCode="General" sourceLinked="1"/><c:majorTickMark val="out"/><c:minorTickMark val="none"/><c:tickLblPos val="nextTo"/><c:crossAx val="${crossId}"/><c:crosses val="autoZero"/>`;
  return kind === 'catAx'
    ? `<c:catAx>${common}<c:auto val="1"/><c:lblAlgn val="ctr"/><c:lblOffset val="100"/><c:noMultiLvlLbl val="0"/></c:catAx>`
    : `<c:valAx>${common}<c:crossBetween val="${crossBetween ?? 'between'}"/></c:valAx>`;
}

/** Sets a title's text, keeping the first run's formatting. */
function writeTitle(holder: Element, text: string, before: Element | null): void {
  const doc = holder.ownerDocument;
  const existing = child(holder, C, 'title');
  const rich = child(child(existing, C, 'tx'), C, 'rich');
  const runs = rich ? Array.from(rich.getElementsByTagNameNS(A, 't')) : [];
  if (existing && runs.length) {
    runs[0].textContent = text;
    runs.slice(1).forEach(t => t.parentElement?.remove());
    children(rich, A, 'p').slice(1).forEach(p => p.remove());
    return;
  }
  existing?.remove();
  const fragment = parseXml(
    `<c:title xmlns:c="${C}" xmlns:a="${A}"><c:tx><c:rich><a:bodyPr/><a:lstStyle/><a:p><a:r><a:rPr lang="en-US"/><a:t></a:t></a:r></a:p></c:rich></c:tx><c:overlay val="0"/></c:title>`,
  );
  fragment.getElementsByTagNameNS(A, 't')[0].textContent = text;
  holder.insertBefore(doc.importNode(fragment.documentElement, true), before);
}

function setChartTitle(chart: Element, text: string): void {
  if (!text) {
    removeChild(chart, 'title');
    setChildFirst(chart, 'autoTitleDeleted', '1');
    return;
  }
  writeTitle(chart, text, chart.firstChild as Element | null);
  setChildAfterTitle(chart);
}

function setChildFirst(chart: Element, name: string, val: string): void {
  const existing = child(chart, C, name);
  if (existing) existing.setAttribute('val', val);
  else chart.insertBefore(el(chart.ownerDocument, name, val), chart.firstChild);
}

function setChildAfterTitle(chart: Element): void {
  const title = child(chart, C, 'title') as Element;
  const auto = child(chart, C, 'autoTitleDeleted') ?? el(chart.ownerDocument, 'autoTitleDeleted', '0');
  auto.setAttribute('val', '0');
  chart.insertBefore(auto, title.nextSibling);
}

function setAxisTitle(axis: Element, text: string): void {
  if (!text) {
    removeChild(axis, 'title');
    return;
  }
  const after = elementChildren(axis).find(e => !['axId', 'scaling', 'delete', 'axPos', 'majorGridlines', 'minorGridlines', 'title'].includes(e.localName)) ?? null;
  writeTitle(axis, text, after);
}

function setLegend(chart: Element, plotArea: Element, position: LegendPosition): void {
  const existing = child(chart, C, 'legend');
  if (position === 'none') {
    existing?.remove();
    return;
  }
  if (existing) {
    const pos = child(existing, C, 'legendPos');
    if (pos) pos.setAttribute('val', position);
    else existing.insertBefore(el(chart.ownerDocument, 'legendPos', position), existing.firstChild);
    return;
  }
  const legend = el(chart.ownerDocument, 'legend');
  legend.appendChild(el(chart.ownerDocument, 'legendPos', position));
  legend.appendChild(el(chart.ownerDocument, 'overlay', '0'));
  chart.insertBefore(legend, plotArea.nextSibling);
}

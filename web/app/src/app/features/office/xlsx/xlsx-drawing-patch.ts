import { NS, children, elementChildren, parseXml, pxToEmu, serializeXml } from '../shared/ooxml';

/**
 * Edits to a kept drawing part: anchors follow what moved in the grid, removed objects go, and charts
 * and pictures made here are added. Anchors nobody touched keep their XML.
 */

export const XDR = 'http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing';
const CHART_URI = 'http://schemas.openxmlformats.org/drawingml/2006/chart';
const A = NS.a;
const R = NS.r;

/** A position as Univer stores it: cells with pixel offsets. */
export interface GridPosition {
  from: { column: number; columnOffset: number; row: number; rowOffset: number };
  to: { column: number; columnOffset: number; row: number; rowOffset: number };
  /** Absolute pixels, for the anchors that use them. */
  left: number;
  top: number;
  width: number;
  height: number;
}

export function emptyDrawingXml(): string {
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n<xdr:wsDr xmlns:xdr="${XDR}" xmlns:a="${A}"></xdr:wsDr>`;
}

export function drawingAnchors(doc: Document): Element[] {
  return elementChildren(doc.documentElement);
}

/** The anchors holding a picture, in order: the n-th is the n-th image the import read. */
export function pictureAnchors(doc: Document): Element[] {
  return drawingAnchors(doc).filter(anchor => children(anchor, XDR, 'pic').length > 0);
}

/** The anchor whose chart uses this relationship id. */
export function chartAnchor(doc: Document, relId: string): Element | null {
  return (
    drawingAnchors(doc).find(anchor => {
      const chart = anchor.getElementsByTagNameNS(CHART_URI, 'chart')[0];
      return chart?.getAttributeNS(R, 'id') === relId;
    }) ?? null
  );
}

function setMarker(marker: Element | undefined, at: GridPosition['from']): void {
  if (!marker) return;
  const set = (tag: string, value: number) => {
    const el = children(marker, XDR, tag)[0];
    if (el) el.textContent = String(value);
  };
  set('col', at.column);
  set('colOff', pxToEmu(at.columnOffset));
  set('row', at.row);
  set('rowOff', pxToEmu(at.rowOffset));
}

/** Moves an anchor of any of the three kinds. */
export function moveAnchor(anchor: Element, position: GridPosition): void {
  setMarker(children(anchor, XDR, 'from')[0], position.from);
  setMarker(children(anchor, XDR, 'to')[0], position.to);
  const ext = children(anchor, XDR, 'ext')[0];
  if (ext) {
    ext.setAttribute('cx', String(pxToEmu(position.width)));
    ext.setAttribute('cy', String(pxToEmu(position.height)));
  }
  const pos = children(anchor, XDR, 'pos')[0];
  if (pos) {
    pos.setAttribute('x', String(pxToEmu(position.left)));
    pos.setAttribute('y', String(pxToEmu(position.top)));
  }
}

function marker(tag: 'from' | 'to', at: GridPosition['from']): string {
  return `<xdr:${tag}><xdr:col>${at.column}</xdr:col><xdr:colOff>${pxToEmu(at.columnOffset)}</xdr:colOff><xdr:row>${at.row}</xdr:row><xdr:rowOff>${pxToEmu(at.rowOffset)}</xdr:rowOff></xdr:${tag}>`;
}

function nextShapeId(doc: Document): number {
  let max = 1;
  for (const el of Array.from(doc.getElementsByTagNameNS(XDR, 'cNvPr'))) max = Math.max(max, Number(el.getAttribute('id')) || 0);
  return max + 1;
}

function append(doc: Document, xml: string): void {
  const fragment = parseXml(`<r xmlns:xdr="${XDR}" xmlns:a="${A}" xmlns:r="${R}">${xml}</r>`);
  for (const node of elementChildren(fragment.documentElement)) doc.documentElement.appendChild(doc.importNode(node, true));
}

const attribute = (text: string) => text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/"/g, '&quot;');

export function appendChartAnchor(doc: Document, position: GridPosition, relId: string, name: string): void {
  append(
    doc,
    `<xdr:twoCellAnchor>${marker('from', position.from)}${marker('to', position.to)}<xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="${nextShapeId(doc)}" name="${attribute(name)}"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr>` +
      `<xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="${CHART_URI}"><c:chart xmlns:c="${CHART_URI}" r:id="${relId}"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:twoCellAnchor>`,
  );
}

export function appendPictureAnchor(doc: Document, position: GridPosition, relId: string, name: string): void {
  append(
    doc,
    `<xdr:twoCellAnchor editAs="oneCell">${marker('from', position.from)}${marker('to', position.to)}<xdr:pic><xdr:nvPicPr><xdr:cNvPr id="${nextShapeId(doc)}" name="${attribute(name)}"/><xdr:cNvPicPr><a:picLocks noChangeAspect="1"/></xdr:cNvPicPr></xdr:nvPicPr>` +
      `<xdr:blipFill><a:blip r:embed="${relId}"/><a:stretch><a:fillRect/></a:stretch></xdr:blipFill><xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="${pxToEmu(position.width)}" cy="${pxToEmu(position.height)}"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr></xdr:pic><xdr:clientData/></xdr:twoCellAnchor>`,
  );
}

export function drawingXml(doc: Document): string {
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>\n${serializeXml(doc.documentElement)}`;
}

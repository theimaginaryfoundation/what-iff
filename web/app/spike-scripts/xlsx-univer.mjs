// Minimal ExcelJS <-> Univer IWorkbookData mapping for the spike: values, formulas, number formats,
// bold/italic, fill colour, merges, column widths, row heights.
import ExcelJS from 'exceljs';

function styleKey(cell) {
  const f = cell.font ?? {}, fill = cell.fill;
  const bg = fill?.type === 'pattern' && fill.fgColor?.argb ? '#' + fill.fgColor.argb.slice(-6) : undefined;
  const s = { bl: f.bold ? 1 : 0, it: f.italic ? 1 : 0, n: cell.numFmt ? { pattern: cell.numFmt } : undefined, bg: bg ? { rgb: bg } : undefined };
  return Object.values(s).some(Boolean) ? s : undefined;
}

export async function toUniver(buffer) {
  const wb = new ExcelJS.Workbook();
  await wb.xlsx.load(buffer);
  const data = { id: 'wb', name: 'workbook', sheetOrder: [], sheets: {}, styles: {} };
  let styleN = 0; const styleIds = new Map();
  wb.eachSheet(ws => {
    const id = `sheet-${ws.id}`; data.sheetOrder.push(id);
    const sheet = { id, name: ws.name, cellData: {}, mergeData: [], columnData: {}, rowData: {}, rowCount: Math.max(ws.rowCount + 20, 100), columnCount: Math.max(ws.columnCount + 10, 26) };
    ws.eachRow({ includeEmpty: false }, (row, r) => {
      if (row.height) sheet.rowData[r - 1] = { h: row.height * 96 / 72 };
      row.eachCell({ includeEmpty: false }, (cell, c) => {
        const out = {};
        const v = cell.value;
        if (v && typeof v === 'object' && 'formula' in v) { out.f = '=' + v.formula; out.v = v.result; }
        else if (v && typeof v === 'object' && 'richText' in v) out.v = v.richText.map(t => t.text).join('');
        else if (v instanceof Date) out.v = v.toISOString();
        else out.v = v;
        if (typeof out.v === 'number') out.t = 2; else if (typeof out.v === 'boolean') out.t = 3; else if (out.v != null) out.t = 1;
        const st = styleKey(cell);
        if (st) { const k = JSON.stringify(st); if (!styleIds.has(k)) { styleIds.set(k, `s${styleN++}`); data.styles[styleIds.get(k)] = st; } out.s = styleIds.get(k); }
        (sheet.cellData[r - 1] ??= {})[c - 1] = out;
      });
    });
    ws.columns?.forEach((col, i) => { if (col?.width) sheet.columnData[i] = { w: col.width * 7 + 5 }; });
    for (const m of Object.values(ws._merges ?? {})) {
      const { top, left, bottom, right } = m.model ?? m;
      sheet.mergeData.push({ startRow: top - 1, endRow: bottom - 1, startColumn: left - 1, endColumn: right - 1 });
    }
    data.sheets[id] = sheet;
  });
  return data;
}

export async function fromUniver(data) {
  const wb = new ExcelJS.Workbook();
  for (const id of data.sheetOrder) {
    const s = data.sheets[id]; const ws = wb.addWorksheet(s.name);
    for (const [r, cols] of Object.entries(s.cellData ?? {})) for (const [c, cell] of Object.entries(cols ?? {})) {
      const x = ws.getCell(Number(r) + 1, Number(c) + 1);
      if (cell.f) x.value = { formula: cell.f.replace(/^=/, ''), result: cell.v }; else x.value = cell.v ?? null;
      const st = typeof cell.s === 'string' ? data.styles[cell.s] : cell.s;
      if (st) {
        if (st.bl || st.it) x.font = { bold: !!st.bl, italic: !!st.it };
        if (st.n?.pattern) x.numFmt = st.n.pattern;
        if (st.bg?.rgb) x.fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: 'FF' + st.bg.rgb.replace('#', '') } };
      }
    }
    for (const [c, col] of Object.entries(s.columnData ?? {})) if (col?.w) ws.getColumn(Number(c) + 1).width = (col.w - 5) / 7;
    for (const [r, row] of Object.entries(s.rowData ?? {})) if (row?.h) ws.getRow(Number(r) + 1).height = row.h * 72 / 96;
    for (const m of s.mergeData ?? []) ws.mergeCells(m.startRow + 1, m.startColumn + 1, m.endRow + 1, m.endColumn + 1);
  }
  return wb;
}

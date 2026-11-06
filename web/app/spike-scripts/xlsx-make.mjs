import fs from 'node:fs';
import ExcelJS from 'exceljs';
const wb = new ExcelJS.Workbook();
const ws = wb.addWorksheet('Sales');
ws.columns = [{ header: 'Item', key: 'item', width: 22 }, { header: 'Qty', key: 'qty', width: 8 }, { header: 'Price', key: 'price', width: 12 }, { header: 'Total', key: 'total', width: 12 }];
ws.getRow(1).font = { bold: true };
ws.getRow(1).fill = { type: 'pattern', pattern: 'solid', fgColor: { argb: 'FFD9E2F3' } };
ws.addRow({ item: 'Widget', qty: 3, price: 4.5 }); ws.addRow({ item: 'Gadget', qty: 10, price: 12 }); ws.addRow({ item: 'Gizmo', qty: 1, price: 99.99 });
for (const r of [2, 3, 4]) { ws.getCell(`D${r}`).value = { formula: `B${r}*C${r}` }; ws.getCell(`C${r}`).numFmt = '$#,##0.00'; ws.getCell(`D${r}`).numFmt = '$#,##0.00'; }
ws.getCell('A6').value = 'Grand total'; ws.getCell('A6').font = { bold: true, italic: true };
ws.getCell('D6').value = { formula: 'SUM(D2:D4)' }; ws.getCell('D6').numFmt = '$#,##0.00';
ws.mergeCells('A8:D8'); ws.getCell('A8').value = 'Merged title row';
ws.getCell('A9').value = new Date(Date.UTC(2026, 10, 6)); ws.getCell('A9').numFmt = 'yyyy-mm-dd';
ws.getCell('B9').value = true;
const ws2 = wb.addWorksheet('Notes'); ws2.getCell('A1').value = 'Second sheet'; ws2.getCell('A2').value = { formula: 'Sales!D6*2' };
// Things the ADR says will be dropped: a chart is not creatable in ExcelJS; data validation is.
ws.getCell('B2').dataValidation = { type: 'whole', operator: 'between', formulae: [0, 1000] };
const buf = await wb.xlsx.writeBuffer();
fs.writeFileSync(new URL('./sample.xlsx', import.meta.url), Buffer.from(buf));
console.log('sample.xlsx bytes', buf.byteLength);

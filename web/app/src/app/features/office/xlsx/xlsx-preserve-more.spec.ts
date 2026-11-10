import ExcelJS from 'exceljs';
import JSZip from 'jszip';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage } from '../shared/ooxml';
import { RESOURCE_DRAWINGS, UniverDrawingsForSheet, readResource } from './univer-sheet-types';
import { workbookToXlsx } from './xlsx-export';
import { xlsxToWorkbook } from './xlsx-import';
import { collectPreservedParts, hasMacros } from './xlsx-preserve';

const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const MAIN = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const rels = (entries: string) =>
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${entries}</Relationships>`;

/** A ExcelJS workbook, then the parts Excel would add for the kinds ExcelJS does not write. */
async function richWorkbook(): Promise<ArrayBuffer> {
  const wb = new ExcelJS.Workbook();
  const ws = wb.addWorksheet('Data', { pageSetup: { orientation: 'landscape', fitToPage: true, fitToWidth: 1, fitToHeight: 0 } });
  ws.addRows([
    ['Region', 'Qty', 'Ext'],
    ['North', 10, { formula: "'[1]Other'!A1", result: 5 }],
    ['South', 25],
    ['East', 15],
  ]);
  ws.headerFooter.oddHeader = '&CQuarterly';
  ws.getRow(3).outlineLevel = 1;
  ws.getRow(4).outlineLevel = 1;
  ws.getColumn(3).outlineLevel = 1;
  ws.views = [{ state: 'normal', zoomScale: 85, tabSelected: true, showRowColHeaders: false } as unknown as ExcelJS.WorksheetView];
  ws.protect('', { selectLockedCells: true, formatCells: false });
  wb.creator = 'Pat';
  wb.title = 'Rich book';
  wb.addWorksheet('Shapes').getCell('A1').value = 'a box lives here';
  const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
  const text = (p: string) => zip.file(p)!.async('string');
  // A table over the data, a sparkline group, and printer settings on the first sheet.
  zip.file(
    'xl/tables/table1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><table xmlns="${MAIN}" id="1" name="Sales" displayName="Sales" ref="A1:B4" totalsRowShown="0"><autoFilter ref="A1:B4"/><tableColumns count="2"><tableColumn id="1" name="Region"/><tableColumn id="2" name="Qty"/></tableColumns><tableStyleInfo name="TableStyleMedium2" showFirstColumn="0" showLastColumn="0" showRowStripes="1" showColumnStripes="0"/></table>`,
  );
  zip.file('xl/printerSettings/printerSettings1.bin', new Uint8Array([1, 2, 3, 4]));
  zip.file(
    'xl/worksheets/sheet1.xml',
    (await text('xl/worksheets/sheet1.xml')).replace(
      '</worksheet>',
      `<tableParts count="1"><tablePart r:id="rIdT1"/></tableParts><extLst><ext uri="{05C60535-1F16-4fd2-B633-F4F36F0B64E0}" xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main"><x14:sparklineGroups xmlns:xm="http://schemas.microsoft.com/office/excel/2006/main"><x14:sparklineGroup displayEmptyCellsAs="gap"><x14:sparklines><x14:sparkline><xm:f>Data!B2:B4</xm:f><xm:sqref>D2</xm:sqref></x14:sparkline></x14:sparklines></x14:sparklineGroup></x14:sparklineGroups></ext></extLst></worksheet>`,
    ),
  );
  const sheet1Rels = zip.file('xl/worksheets/_rels/sheet1.xml.rels');
  const existingSheet1Rels = sheet1Rels
    ? (await sheet1Rels.async('string')).replace(/<\/Relationships>$/, '')
    : rels('').replace(/<\/Relationships>$/, '');
  zip.file(
    'xl/worksheets/_rels/sheet1.xml.rels',
    `${existingSheet1Rels}<Relationship Id="rIdT1" Type="${R}/table" Target="../tables/table1.xml"/><Relationship Id="rIdP1" Type="${R}/printerSettings" Target="../printerSettings/printerSettings1.bin"/></Relationships>`,
  );
  // A text box on the second sheet.
  zip.file(
    'xl/drawings/drawing1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><xdr:twoCellAnchor><xdr:from><xdr:col>2</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>2</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>6</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>8</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to><xdr:sp macro="" textlink=""><xdr:nvSpPr><xdr:cNvPr id="2" name="TextBox 1"/><xdr:cNvSpPr txBox="1"/></xdr:nvSpPr><xdr:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></xdr:spPr><xdr:txBody><a:bodyPr/><a:p><a:r><a:t>Hello</a:t></a:r></a:p></xdr:txBody></xdr:sp><xdr:clientData/></xdr:twoCellAnchor></xdr:wsDr>`,
  );
  zip.file(
    'xl/worksheets/sheet2.xml',
    (await text('xl/worksheets/sheet2.xml')).replace('</worksheet>', '<drawing r:id="rId1"/></worksheet>'),
  );
  zip.file('xl/worksheets/_rels/sheet2.xml.rels', rels(`<Relationship Id="rId1" Type="${R}/drawing" Target="../drawings/drawing1.xml"/>`));
  // An external link, a macro project and a custom XML part on the workbook; custom properties on the package.
  zip.file(
    'xl/externalLinks/externalLink1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><externalLink xmlns="${MAIN}" xmlns:r="${R}"><externalBook r:id="rId1"><sheetNames><sheetName val="Other"/></sheetNames></externalBook></externalLink>`,
  );
  zip.file(
    'xl/externalLinks/_rels/externalLink1.xml.rels',
    rels(`<Relationship Id="rId1" Type="${R}/externalLinkPath" Target="file:///C:/other.xlsx" TargetMode="External"/>`),
  );
  zip.file('xl/vbaProject.bin', new Uint8Array([0xd0, 0xcf, 0x11, 0xe0]));
  zip.file('customXml/item1.xml', '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><root xmlns="urn:x"><v>1</v></root>');
  zip.file(
    'customXml/itemProps1.xml',
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><ds:datastoreItem ds:itemID="{1}" xmlns:ds="http://schemas.openxmlformats.org/officeDocument/2006/customXml"><ds:schemaRefs/></ds:datastoreItem>',
  );
  zip.file('customXml/_rels/item1.xml.rels', rels(`<Relationship Id="rId1" Type="${R}/customXmlProps" Target="itemProps1.xml"/>`));
  zip.file(
    'docProps/custom.xml',
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="Label"><vt:lpwstr>Internal</vt:lpwstr></property></Properties>',
  );
  zip.file(
    'xl/workbook.xml',
    (await text('xl/workbook.xml')).replace('<calcPr', '<externalReferences><externalReference r:id="rIdX1"/></externalReferences><calcPr'),
  );
  zip.file(
    'xl/_rels/workbook.xml.rels',
    (await text('xl/_rels/workbook.xml.rels')).replace(
      '</Relationships>',
      `<Relationship Id="rIdX1" Type="${R}/externalLink" Target="externalLinks/externalLink1.xml"/><Relationship Id="rIdV1" Type="${R}/vbaProject" Target="vbaProject.bin"/><Relationship Id="rIdC1" Type="${R}/customXml" Target="../customXml/item1.xml"/></Relationships>`,
    ),
  );
  zip.file(
    '_rels/.rels',
    (await text('_rels/.rels')).replace(
      '</Relationships>',
      `<Relationship Id="rIdCP" Type="${R}/custom-properties" Target="docProps/custom.xml"/></Relationships>`,
    ),
  );
  zip.file(
    '[Content_Types].xml',
    (await text('[Content_Types].xml'))
      .replace(
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml',
        'application/vnd.ms-excel.sheet.macroEnabled.main+xml',
      )
      .replace(
        '</Types>',
        '<Default Extension="bin" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.printerSettings"/><Override PartName="/xl/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"/><Override PartName="/xl/tables/table1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml"/><Override PartName="/xl/drawings/drawing1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/><Override PartName="/xl/externalLinks/externalLink1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.externalLink+xml"/><Override PartName="/customXml/itemProps1.xml" ContentType="application/vnd.openxmlformats-officedocument.customXmlProperties+xml"/><Override PartName="/docProps/custom.xml" ContentType="application/vnd.openxmlformats-officedocument.custom-properties+xml"/></Types>',
      ),
  );
  return zip.generateAsync({ type: 'arraybuffer' });
}

describe('xlsx preservation of tables, shapes, links, macros and document parts', () => {
  it('collects every kind from the original', async () => {
    const preserved = await collectPreservedParts(await OoxmlPackage.open(await richWorkbook()));
    expect(preserved.bySheet['Data']).toMatchObject({
      tables: ['xl/tables/table1.xml'],
      printerSettings: 'xl/printerSettings/printerSettings1.bin',
    });
    expect(preserved.bySheet['Data'].exts).toHaveLength(1);
    expect(preserved.bySheet['Data'].exts[0]).toContain('sparklineGroups');
    expect(preserved.bySheet['Shapes'].drawing).toMatchObject({ path: 'xl/drawings/drawing1.xml', charts: [], shapes: 1 });
    expect(preserved.externalLinks).toEqual(['xl/externalLinks/externalLink1.xml']);
    expect(preserved.vbaProject).toBe('xl/vbaProject.bin');
    expect(preserved.customXml).toEqual(['customXml/item1.xml']);
    expect(preserved.customProperties).toBe('docProps/custom.xml');
    expect(Object.keys(preserved.parts).sort()).toEqual([
      'customXml/item1.xml',
      'customXml/itemProps1.xml',
      'docProps/custom.xml',
      'xl/drawings/drawing1.xml',
      'xl/externalLinks/externalLink1.xml',
      'xl/printerSettings/printerSettings1.bin',
      'xl/tables/table1.xml',
      'xl/vbaProject.bin',
    ]);
    expect(preserved.parts['xl/vbaProject.bin'].contentType).toBe('application/vnd.ms-office.vbaProject');
    expect(preserved.parts['xl/printerSettings/printerSettings1.bin'].contentType).toBe(
      'application/vnd.openxmlformats-officedocument.spreadsheetml.printerSettings',
    );
    expect(hasMacros(preserved)).toBe(true);
  });

  it('says what is kept but not editable, and what the editor has no model for', async () => {
    const { report, workbook, preserved } = await xlsxToWorkbook(await richWorkbook(), { id: 'wb', name: 'rich.xlsm' });
    expect(report.dropped).toEqual([]);
    expect(report.warnings.map(w => `${w.feature}|${w.where ?? ''}`).sort()).toEqual(
      [
        'Links to other workbooks are kept in the file|',
        'Macros are kept in the file but cannot run here|',
        'Shapes and text boxes are kept in the file but not shown here|Shapes',
        'Sparklines are kept in the file but not shown here|Data',
        'Tables are kept in the file but not editable here; edits to their headers do not update them|Data',
      ].sort(),
    );
    expect(readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS)).toBeNull();
    const data = workbook.sheets[workbook.sheetOrder[0]];
    expect(data.zoomRatio).toBe(0.85);
    expect(preserved?.extras?.sheets['Data']).toMatchObject({
      pageSetup: { orientation: 'landscape', fitToWidth: 1 },
      headerFooter: { oddHeader: '&CQuarterly' },
      view: { showRowColHeaders: false },
      rowOutline: { 2: { level: 1 }, 3: { level: 1 } },
      columnOutline: { 2: { level: 1 } },
    });
    expect(preserved?.extras?.sheets['Data'].sheetProtection).toMatchObject({ sheet: true });
    expect(preserved?.extras?.document).toMatchObject({ creator: 'Pat', title: 'Rich book' });
    expect(preserved?.extras?.themes?.['theme1']).toContain('<a:theme');
  });

  it('writes everything back and the result opens in ExcelJS with it all in place', async () => {
    const first = await xlsxToWorkbook(await richWorkbook(), { id: 'wb', name: 'rich.xlsm' });
    first.workbook.sheets[first.workbook.sheetOrder[0]].cellData[1][1] = { v: 11, t: 2 };
    first.workbook.sheets[first.workbook.sheetOrder[0]].zoomRatio = 1.2;
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const pkg = await OoxmlPackage.open(saved.bytes);
    const paths = pkg.paths();
    expect(paths).toEqual(
      expect.arrayContaining([
        'xl/tables/table1.xml',
        'xl/printerSettings/printerSettings1.bin',
        'xl/drawings/drawingP1.xml',
        'xl/externalLinks/externalLink1.xml',
        'xl/externalLinks/_rels/externalLink1.xml.rels',
        'xl/vbaProject.bin',
        'customXml/item1.xml',
        'customXml/itemProps1.xml',
        'customXml/_rels/item1.xml.rels',
        'docProps/custom.xml',
      ]),
    );
    const sheet1 = (await pkg.text('xl/worksheets/sheet1.xml'))!;
    expect(sheet1).toMatch(/<tableParts count="1"><tablePart r:id="rId\d+"\/><\/tableParts>/);
    expect(sheet1).toContain('<x14:sparklineGroups');
    expect(sheet1).toMatch(/<extLst>.*<\/extLst><\/worksheet>$/);
    expect(sheet1).toMatch(/<pageSetup [^>]*orientation="landscape"[^>]*r:id="rId\d+"/);
    expect(sheet1).toContain('zoomScale="120"');
    expect(sheet1).toContain('<sheetProtection');
    expect(sheet1).toMatch(/<row r="3"[^>]*outlineLevel="1"/);
    expect(sheet1).toContain('&amp;CQuarterly');
    const sheet1Rels = [...(await pkg.rels('xl/worksheets/sheet1.xml')).values()];
    expect(sheet1Rels.find(r => r.type.endsWith('/table'))?.target).toBe('../tables/table1.xml');
    expect(sheet1Rels.find(r => r.type.endsWith('/printerSettings'))?.target).toBe('../printerSettings/printerSettings1.bin');
    const sheet2 = (await pkg.text('xl/worksheets/sheet2.xml'))!;
    expect(sheet2).toMatch(/<drawing r:id="rId\d+"\/>/);
    const workbookXml = (await pkg.text('xl/workbook.xml'))!;
    expect(workbookXml).toMatch(/<externalReferences><externalReference r:id="rId\d+"\/><\/externalReferences><calcPr/);
    const wbRels = [...(await pkg.rels('xl/workbook.xml')).values()];
    expect(wbRels.find(r => r.type.endsWith('/vbaProject'))?.target).toBe('vbaProject.bin');
    expect(wbRels.find(r => r.type.endsWith('/customXml'))?.target).toBe('../customXml/item1.xml');
    expect(wbRels.find(r => r.type.endsWith('/externalLink'))?.target).toBe('externalLinks/externalLink1.xml');
    expect([...(await pkg.rels('')).values()].find(r => r.type.endsWith('/custom-properties'))?.target).toBe('docProps/custom.xml');
    const ct = (await pkg.text('[Content_Types].xml'))!;
    expect(ct).toContain('PartName="/xl/workbook.xml" ContentType="application/vnd.ms-excel.sheet.macroEnabled.main+xml"');
    expect(ct).toContain('PartName="/xl/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"');
    expect(ct).toContain('PartName="/xl/tables/table1.xml"');
    expect(ct).toContain('PartName="/docProps/custom.xml"');
    expect((await pkg.text('xl/theme/theme1.xml'))!).toContain('<a:theme');
    expect((await pkg.text('docProps/core.xml'))!).toContain('<dc:creator>Pat</dc:creator>');
    // ExcelJS reads it again with the edit, the table, the protection and the outline.
    const excel = new ExcelJS.Workbook();
    await excel.xlsx.load(saved.bytes as never);
    const data = excel.getWorksheet('Data')!;
    expect(data.getCell('B2').value).toBe(11);
    expect(data.getTables().map(t => (t as unknown as { name: string }).name)).toEqual(['Sales']);
    expect(data.getRow(3).outlineLevel).toBe(1);
    expect((data as unknown as { sheetProtection: { sheet?: boolean } }).sheetProtection?.sheet).toBe(true);
    expect(data.pageSetup.orientation).toBe('landscape');
    expect(data.getCell('C2').formula).toBe("'[1]Other'!A1");
    // And a second round trip keeps the same names.
    const second = await xlsxToWorkbook(saved.bytes, { id: 'wb', name: 'rich.xlsm' });
    expect(hasMacros(second.preserved)).toBe(true);
    const again = await OoxmlPackage.open((await workbookToXlsx(second.workbook, second.preserved)).bytes);
    expect(again.paths()).toEqual(expect.arrayContaining(['xl/drawings/drawingP1.xml', 'xl/tables/table1.xml', 'xl/vbaProject.bin']));
    expect(again.paths().filter(p => /drawingP/.test(p))).toEqual(['xl/drawings/drawingP1.xml']);
  });

  it('keeps slicers rather than dropping them', async () => {
    const wb = new ExcelJS.Workbook();
    const ws = wb.addWorksheet('S');
    ws.getCell('A1').value = 'x';
    ws.getCell('A1').note = 'legacy';
    const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
    const sheetRels = await zip.file('xl/worksheets/_rels/sheet1.xml.rels')!.async('string');
    zip.file(
      'xl/worksheets/_rels/sheet1.xml.rels',
      sheetRels.replace(
        '</Relationships>',
        '<Relationship Id="rIdTC" Type="http://schemas.microsoft.com/office/2017/10/relationships/threadedComment" Target="../threadedComments/threadedComment1.xml"/></Relationships>',
      ),
    );
    zip.file(
      'xl/threadedComments/threadedComment1.xml',
      '<?xml version="1.0" encoding="UTF-8" standalone="yes"?><ThreadedComments xmlns="http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments"/>',
    );
    zip.file(
      'xl/worksheets/sheet1.xml',
      (await zip.file('xl/worksheets/sheet1.xml')!.async('string')).replace(
        '</worksheet>',
        '<extLst><ext uri="{A8765BA9-456A-4dab-B4F3-ACF838C121DE}" xmlns:x14="http://schemas.microsoft.com/office/spreadsheetml/2009/9/main"><x14:slicerList/></ext></extLst></worksheet>',
      ),
    );
    const { report } = await xlsxToWorkbook(await zip.generateAsync({ type: 'arraybuffer' }), { id: 'wb', name: 's.xlsx' });
    expect(report.dropped).toEqual([]);
  });
});

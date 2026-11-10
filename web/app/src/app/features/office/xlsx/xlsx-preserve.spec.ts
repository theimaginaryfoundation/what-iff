import ExcelJS from 'exceljs';
import JSZip from 'jszip';
import { describe, expect, it } from 'vitest';

import { OoxmlPackage } from '../shared/ooxml';
import { RESOURCE_DRAWINGS, UniverDrawingsForSheet, readResource } from './univer-sheet-types';
import { workbookToXlsx } from './xlsx-export';
import { xlsxToWorkbook } from './xlsx-import';
import { collectPreservedParts, hasPreservedParts } from './xlsx-preserve';

const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const rels = (entries: string) =>
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${entries}</Relationships>`;

/** An ExcelJS workbook with a bar chart on "Data" and a pivot table on "Pivot", added as the OOXML parts Excel writes. */
async function chartAndPivotWorkbook(): Promise<ArrayBuffer> {
  const wb = new ExcelJS.Workbook();
  const data = wb.addWorksheet('Data');
  data.addRows([
    ['Region', 'Qty'],
    ['North', 10],
    ['South', 25],
    ['East', 15],
  ]);
  const pivot = wb.addWorksheet('Pivot');
  pivot.addRows([[], [], ['Row Labels', 'Sum of Qty'], ['East', 15], ['North', 10], ['South', 25], ['Grand Total', 50]]);
  const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
  const text = (p: string) => zip.file(p)!.async('string');
  zip.file(
    'xl/charts/chart1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><c:chartSpace xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="${R}"><c:chart><c:title><c:tx><c:rich><a:bodyPr/><a:p><a:r><a:t>Quantity by region</a:t></a:r></a:p></c:rich></c:tx></c:title><c:autoTitleDeleted val="0"/><c:plotArea><c:barChart><c:barDir val="col"/><c:grouping val="clustered"/><c:ser><c:idx val="0"/><c:order val="0"/><c:tx><c:strRef><c:f>Data!$B$1</c:f><c:strCache><c:ptCount val="1"/><c:pt idx="0"><c:v>Qty</c:v></c:pt></c:strCache></c:strRef></c:tx><c:spPr><a:solidFill><a:srgbClr val="4472C4"/></a:solidFill></c:spPr><c:cat><c:strRef><c:f>Data!$A$2:$A$4</c:f><c:strCache><c:ptCount val="3"/><c:pt idx="0"><c:v>North</c:v></c:pt><c:pt idx="1"><c:v>South</c:v></c:pt><c:pt idx="2"><c:v>East</c:v></c:pt></c:strCache></c:strRef></c:cat><c:val><c:numRef><c:f>Data!$B$2:$B$4</c:f><c:numCache><c:formatCode>General</c:formatCode><c:ptCount val="3"/><c:pt idx="0"><c:v>10</c:v></c:pt><c:pt idx="1"><c:v>25</c:v></c:pt><c:pt idx="2"><c:v>15</c:v></c:pt></c:numCache></c:numRef></c:val></c:ser><c:axId val="1"/><c:axId val="2"/></c:barChart><c:catAx><c:axId val="1"/><c:crossAx val="2"/></c:catAx><c:valAx><c:axId val="2"/><c:crossAx val="1"/></c:valAx></c:plotArea></c:chart></c:chartSpace>`,
  );
  zip.file(
    'xl/drawings/drawing1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><xdr:wsDr xmlns:xdr="http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><xdr:twoCellAnchor><xdr:from><xdr:col>3</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>1</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>10</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>16</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to><xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="2" name="Chart 1"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart"><c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" xmlns:r="${R}" r:id="rId1"/></a:graphicData></a:graphic></xdr:graphicFrame><xdr:clientData/></xdr:twoCellAnchor></xdr:wsDr>`,
  );
  zip.file('xl/drawings/_rels/drawing1.xml.rels', rels(`<Relationship Id="rId1" Type="${R}/chart" Target="../charts/chart1.xml"/>`));
  zip.file(
    'xl/worksheets/sheet1.xml',
    (await text('xl/worksheets/sheet1.xml')).replace('</worksheet>', '<drawing r:id="rId1"/></worksheet>'),
  );
  zip.file('xl/worksheets/_rels/sheet1.xml.rels', rels(`<Relationship Id="rId1" Type="${R}/drawing" Target="../drawings/drawing1.xml"/>`));
  zip.file(
    'xl/pivotCache/pivotCacheDefinition1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><pivotCacheDefinition xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="${R}" r:id="rId1" refreshOnLoad="1" recordCount="3"><cacheSource type="worksheet"><worksheetSource ref="A1:B4" sheet="Data"/></cacheSource><cacheFields count="2"><cacheField name="Region" numFmtId="0"><sharedItems count="3"><s v="North"/><s v="South"/><s v="East"/></sharedItems></cacheField><cacheField name="Qty" numFmtId="0"><sharedItems containsNumber="1" containsInteger="1" minValue="10" maxValue="25"/></cacheField></cacheFields></pivotCacheDefinition>`,
  );
  zip.file(
    'xl/pivotCache/_rels/pivotCacheDefinition1.xml.rels',
    rels(`<Relationship Id="rId1" Type="${R}/pivotCacheRecords" Target="pivotCacheRecords1.xml"/>`),
  );
  zip.file(
    'xl/pivotCache/pivotCacheRecords1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><pivotCacheRecords xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3"><r><x v="0"/><n v="10"/></r><r><x v="1"/><n v="25"/></r><r><x v="2"/><n v="15"/></r></pivotCacheRecords>`,
  );
  zip.file(
    'xl/pivotTables/pivotTable1.xml',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><pivotTableDefinition xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" name="PivotTable1" cacheId="1" dataCaption="Values"><location ref="A3:B7" firstHeaderRow="1" firstDataRow="1" firstDataCol="1"/><pivotFields count="2"><pivotField axis="axisRow" showAll="0"><items count="4"><item x="2"/><item x="0"/><item x="1"/><item t="default"/></items></pivotField><pivotField dataField="1" showAll="0"/></pivotFields><rowFields count="1"><field x="0"/></rowFields><rowItems count="4"><i><x/></i><i><x v="1"/></i><i><x v="2"/></i><i t="grand"><x/></i></rowItems><colItems count="1"><i/></colItems><dataFields count="1"><dataField name="Sum of Qty" fld="1" baseField="0" baseItem="0"/></dataFields></pivotTableDefinition>`,
  );
  zip.file(
    'xl/pivotTables/_rels/pivotTable1.xml.rels',
    rels(`<Relationship Id="rId1" Type="${R}/pivotCacheDefinition" Target="../pivotCache/pivotCacheDefinition1.xml"/>`),
  );
  zip.file(
    'xl/worksheets/_rels/sheet2.xml.rels',
    rels(`<Relationship Id="rId1" Type="${R}/pivotTable" Target="../pivotTables/pivotTable1.xml"/>`),
  );
  zip.file(
    'xl/workbook.xml',
    (await text('xl/workbook.xml')).replace(
      '</workbook>',
      '<pivotCaches><pivotCache cacheId="1" r:id="rIdPivot1"/></pivotCaches></workbook>',
    ),
  );
  zip.file(
    'xl/_rels/workbook.xml.rels',
    (await text('xl/_rels/workbook.xml.rels')).replace(
      '</Relationships>',
      `<Relationship Id="rIdPivot1" Type="${R}/pivotCacheDefinition" Target="pivotCache/pivotCacheDefinition1.xml"/></Relationships>`,
    ),
  );
  zip.file(
    '[Content_Types].xml',
    (await text('[Content_Types].xml')).replace(
      '</Types>',
      '<Override PartName="/xl/charts/chart1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/><Override PartName="/xl/drawings/drawing1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawing+xml"/><Override PartName="/xl/pivotTables/pivotTable1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.pivotTable+xml"/><Override PartName="/xl/pivotCache/pivotCacheDefinition1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheDefinition+xml"/><Override PartName="/xl/pivotCache/pivotCacheRecords1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheRecords+xml"/></Types>',
    ),
  );
  return zip.generateAsync({ type: 'arraybuffer' });
}

describe('xlsx charts and pivot tables', () => {
  it('finds the chart, its drawing and the pivot parts in the original', async () => {
    const preserved = await collectPreservedParts(await OoxmlPackage.open(await chartAndPivotWorkbook()));
    expect(hasPreservedParts(preserved)).toBe(true);
    expect(preserved.bySheet['Data'].drawing?.charts).toEqual([
      {
        from: { col: 3, colOff: 0, row: 1, rowOff: 0 },
        to: { col: 10, colOff: 0, row: 16, rowOff: 0 },
        chartPath: 'xl/charts/chart1.xml',
        name: 'Chart 1',
      },
    ]);
    expect(preserved.bySheet['Pivot'].pivotTables).toEqual(['xl/pivotTables/pivotTable1.xml']);
    expect(Object.keys(preserved.parts).sort()).toEqual([
      'xl/charts/chart1.xml',
      'xl/drawings/drawing1.xml',
      'xl/pivotCache/pivotCacheDefinition1.xml',
      'xl/pivotCache/pivotCacheRecords1.xml',
      'xl/pivotTables/pivotTable1.xml',
    ]);
    expect(preserved.pivotCaches).toEqual([{ cacheId: '1', path: 'xl/pivotCache/pivotCacheDefinition1.xml' }]);
  });

  it('draws the chart from its cached data at its anchor and says what is kept but not editable', async () => {
    const { workbook, report, preserved } = await xlsxToWorkbook(await chartAndPivotWorkbook(), { id: 'wb', name: 'cp.xlsx' });
    expect(preserved && hasPreservedParts(preserved)).toBe(true);
    expect(report.warnings.map(w => w.feature)).toEqual(
      expect.arrayContaining([expect.stringContaining('Charts are shown as pictures'), expect.stringContaining('Pivot tables are kept')]),
    );
    const drawings = readResource<Record<string, UniverDrawingsForSheet>>(workbook, RESOURCE_DRAWINGS)!;
    const data = drawings[workbook.sheetOrder[0]];
    expect(data.order).toEqual(['chart-sheet-1-0']);
    const chart = data.data['chart-sheet-1-0'];
    expect(chart.sheetTransform.from).toEqual({ column: 3, columnOffset: 0, row: 1, rowOffset: 0 });
    expect(chart.source.startsWith('data:image/svg+xml')).toBe(true);
    const svg = decodeURIComponent(chart.source.split(',')[1]);
    expect(svg).toContain('Quantity by region');
    expect(svg).toContain('<rect');
    expect(svg).toContain('North');
  });

  it('writes the chart and pivot parts back into the saved file, twice over', async () => {
    const first = await xlsxToWorkbook(await chartAndPivotWorkbook(), { id: 'wb', name: 'cp.xlsx' });
    first.workbook.sheets[first.workbook.sheetOrder[0]].cellData[1][1] = { v: 11, t: 2 };
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const pkg = await OoxmlPackage.open(saved.bytes);
    const paths = pkg.paths();
    expect(paths).toEqual(
      expect.arrayContaining([
        'xl/charts/chart1.xml',
        'xl/drawings/drawingP1.xml',
        'xl/drawings/_rels/drawingP1.xml.rels',
        'xl/pivotTables/pivotTable1.xml',
        'xl/pivotCache/pivotCacheDefinition1.xml',
        'xl/pivotCache/pivotCacheRecords1.xml',
      ]),
    );
    expect(await pkg.text('xl/drawings/_rels/drawingP1.xml.rels')).toContain('Target="../charts/chart1.xml"');
    const sheet1 = (await pkg.text('xl/worksheets/sheet1.xml'))!;
    expect(sheet1).toMatch(/<drawing r:id="rId\d+"\/><\/worksheet>$/);
    const sheet1Rels = [...(await pkg.rels('xl/worksheets/sheet1.xml')).values()];
    expect(sheet1Rels.find(r => r.type.endsWith('/drawing'))?.target).toBe('../drawings/drawingP1.xml');
    const sheet2Rels = [...(await pkg.rels('xl/worksheets/sheet2.xml')).values()];
    expect(sheet2Rels.find(r => r.type.endsWith('/pivotTable'))?.target).toBe('../pivotTables/pivotTable1.xml');
    expect(await pkg.text('xl/workbook.xml')).toContain('<pivotCaches><pivotCache cacheId="1"');
    expect(await pkg.text('[Content_Types].xml')).toContain('PartName="/xl/charts/chart1.xml"');
    // The edited cell is there, and the file reads again with everything still in place.
    const second = await xlsxToWorkbook(saved.bytes, { id: 'wb', name: 'cp.xlsx' });
    expect(second.workbook.sheets[second.workbook.sheetOrder[0]].cellData[1][1]).toMatchObject({ v: 11 });
    expect(second.preserved?.bySheet['Data'].drawing?.charts[0].chartPath).toBe('xl/charts/chart1.xml');
    const again = await workbookToXlsx(second.workbook, second.preserved);
    expect((await OoxmlPackage.open(again.bytes)).paths()).toEqual(
      expect.arrayContaining(['xl/charts/chart1.xml', 'xl/drawings/drawingP1.xml', 'xl/pivotTables/pivotTable1.xml']),
    );
    const excel = new ExcelJS.Workbook();
    await excel.xlsx.load(again.bytes as never);
    expect(excel.worksheets.map(w => w.name)).toEqual(['Data', 'Pivot']);
  });

  it('leaves a plain workbook alone', async () => {
    const wb = new ExcelJS.Workbook();
    wb.addWorksheet('S').getCell('A1').value = 1;
    const result = await xlsxToWorkbook(await wb.xlsx.writeBuffer(), { id: 'wb', name: 'plain.xlsx' });
    expect(hasPreservedParts(result.preserved)).toBe(false);
    expect(result.report.warnings).toEqual([]);
  });
});

import ExcelJS from 'exceljs';
import JSZip from 'jszip';

const SS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const X14 = 'http://schemas.microsoft.com/office/spreadsheetml/2009/9/main';
const X15 = 'http://schemas.microsoft.com/office/spreadsheetml/2010/11/main';
const SLE = 'http://schemas.microsoft.com/office/drawing/2010/slicer';
const SLE15 = 'http://schemas.microsoft.com/office/drawing/2012/slicer';
const MC = 'http://schemas.openxmlformats.org/markup-compatibility/2006';
const XDR = 'http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing';
const A = 'http://schemas.openxmlformats.org/drawingml/2006/main';
const HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>';
const rels = (entries: string) => `${HEAD}<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${entries}</Relationships>`;
const rel = (id: string, type: string, target: string) => `<Relationship Id="${id}" Type="${type}" Target="${target}"/>`;

const frame = (name: string, requires: string, ns: string, col: number) =>
  `<xdr:twoCellAnchor editAs="oneCell"><xdr:from><xdr:col>${col}</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>1</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:from><xdr:to><xdr:col>${col + 3}</xdr:col><xdr:colOff>0</xdr:colOff><xdr:row>12</xdr:row><xdr:rowOff>0</xdr:rowOff></xdr:to>` +
  `<mc:AlternateContent xmlns:mc="${MC}" ${ns}><mc:Choice Requires="${requires}"><xdr:graphicFrame macro=""><xdr:nvGraphicFramePr><xdr:cNvPr id="${col + 2}" name="${name}"/><xdr:cNvGraphicFramePr/></xdr:nvGraphicFramePr><xdr:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/></xdr:xfrm><a:graphic><a:graphicData uri="${SLE}"><sle:slicer xmlns:sle="${SLE}" name="${name}"/></a:graphicData></a:graphic></xdr:graphicFrame></mc:Choice>` +
  `<mc:Fallback xmlns=""><xdr:sp macro="" textlink=""><xdr:nvSpPr><xdr:cNvPr id="0" name=""/><xdr:cNvSpPr/></xdr:nvSpPr><xdr:spPr/></xdr:sp></mc:Fallback></mc:AlternateContent><xdr:clientData/></xdr:twoCellAnchor>`;

export const SOURCE_ROWS: (string | number)[][] = [
  ['Region', 'Year', 'Qty'],
  ['North', 2024, 10],
  ['South', 2024, 25],
  ['East', 2025, 15],
  ['North', 2025, 5],
];

/**
 * A workbook with a pivot-backed slicer on "Pivot" and a table-backed one on "Data", written after the
 * shape of the MS-XLSX examples. The sheets get unusual ids, to show they are rewritten on save.
 */
export async function slicerWorkbook(): Promise<ArrayBuffer> {
  const wb = new ExcelJS.Workbook();
  wb.addWorksheet('Data').addRows(SOURCE_ROWS);
  wb.addWorksheet('Pivot').addRows([[], [], ['Row Labels', 'Sum of Qty'], ['East', 15], ['North', 15], ['South', 25], ['Grand Total', 55]]);
  const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
  const text = (p: string) => zip.file(p)!.async('string');
  zip.file('xl/pivotCache/pivotCacheDefinition1.xml', `${HEAD}<pivotCacheDefinition xmlns="${SS}" xmlns:r="${R}" r:id="rId1" recordCount="4"><cacheSource type="worksheet"><worksheetSource ref="A1:C5" sheet="Data"/></cacheSource><cacheFields count="3"><cacheField name="Region" numFmtId="0"><sharedItems count="3"><s v="North"/><s v="South"/><s v="East"/></sharedItems></cacheField><cacheField name="Year" numFmtId="0"><sharedItems containsSemiMixedTypes="0" containsString="0" containsNumber="1" containsInteger="1" minValue="2024" maxValue="2025"/></cacheField><cacheField name="Qty" numFmtId="0"><sharedItems containsSemiMixedTypes="0" containsString="0" containsNumber="1" containsInteger="1" minValue="5" maxValue="25"/></cacheField></cacheFields><extLst><ext uri="{725AE2AE-9491-48be-B2B4-4EB974FC3084}" xmlns:x14="${X14}"><x14:pivotCacheDefinition pivotCacheId="77"/></ext></extLst></pivotCacheDefinition>`);
  zip.file('xl/pivotCache/_rels/pivotCacheDefinition1.xml.rels', rels(rel('rId1', `${R}/pivotCacheRecords`, 'pivotCacheRecords1.xml')));
  zip.file('xl/pivotCache/pivotCacheRecords1.xml', `${HEAD}<pivotCacheRecords xmlns="${SS}" xmlns:r="${R}" count="4"><r><x v="0"/><n v="2024"/><n v="10"/></r><r><x v="1"/><n v="2024"/><n v="25"/></r><r><x v="2"/><n v="2025"/><n v="15"/></r><r><x v="0"/><n v="2025"/><n v="5"/></r></pivotCacheRecords>`);
  zip.file('xl/pivotTables/pivotTable1.xml', `${HEAD}<pivotTableDefinition xmlns="${SS}" name="Sales" cacheId="11" dataCaption="Values" updatedVersion="6" minRefreshableVersion="3" createdVersion="6" indent="0" outline="1" outlineData="1"><location ref="A3:B7" firstHeaderRow="1" firstDataRow="1" firstDataCol="1"/><pivotFields count="3"><pivotField axis="axisRow" showAll="0"><items count="4"><item x="2"/><item x="0"/><item x="1"/><item t="default"/></items></pivotField><pivotField showAll="0"/><pivotField dataField="1" showAll="0"/></pivotFields><rowFields count="1"><field x="0"/></rowFields><rowItems count="4"><i><x/></i><i><x v="1"/></i><i><x v="2"/></i><i t="grand"><x/></i></rowItems><colItems count="1"><i/></colItems><dataFields count="1"><dataField name="Sum of Qty" fld="2" baseField="0" baseItem="0"/></dataFields><pivotTableStyleInfo name="PivotStyleLight16" showRowHeaders="1" showColHeaders="1" showRowStripes="0" showColStripes="0" showLastColumn="1"/></pivotTableDefinition>`);
  zip.file('xl/pivotTables/_rels/pivotTable1.xml.rels', rels(rel('rId1', `${R}/pivotCacheDefinition`, '../pivotCache/pivotCacheDefinition1.xml')));
  zip.file('xl/tables/table1.xml', `${HEAD}<table xmlns="${SS}" id="5" name="Sales" displayName="SalesTable" ref="A1:C5" totalsRowShown="0"><autoFilter ref="A1:C5"/><tableColumns count="3"><tableColumn id="1" name="Region"/><tableColumn id="2" name="Year"/><tableColumn id="3" name="Qty"/></tableColumns><tableStyleInfo name="TableStyleMedium2" showFirstColumn="0" showLastColumn="0" showRowStripes="1" showColumnStripes="0"/></table>`);
  zip.file('xl/slicers/slicer1.xml', `${HEAD}<slicers xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}"><slicer name="Region" cache="Slicer_Region" caption="Region" rowHeight="241300"/></slicers>`);
  zip.file('xl/slicers/slicer2.xml', `${HEAD}<slicers xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}"><slicer name="Year" cache="Slicer_Year" caption="Year" rowHeight="241300"/></slicers>`);
  zip.file('xl/slicerCaches/slicerCache1.xml', `${HEAD}<slicerCacheDefinition xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}" name="Slicer_Region" sourceName="Region"><pivotTables><pivotTable tabId="9" name="Sales"/></pivotTables><data><tabular pivotCacheId="77"><items count="3"><i x="2" s="1"/><i x="0" s="1"/><i x="1" s="1"/></items></tabular></data></slicerCacheDefinition>`);
  zip.file('xl/slicerCaches/slicerCache2.xml', `${HEAD}<slicerCacheDefinition xmlns="${X14}" xmlns:mc="${MC}" mc:Ignorable="x" xmlns:x="${SS}" name="Slicer_Year" sourceName="Year"><extLst><x:ext uri="{2F2917AC-EB37-4324-AD4E-5DD8C200BD13}" xmlns:x15="${X15}"><x15:tableSlicerCache tableId="5" column="2"/></x:ext></extLst></slicerCacheDefinition>`);
  zip.file('xl/drawings/drawing1.xml', `${HEAD}<xdr:wsDr xmlns:xdr="${XDR}" xmlns:a="${A}">${frame('Region', 'a14', 'xmlns:a14="http://schemas.microsoft.com/office/drawing/2010/main"', 3)}</xdr:wsDr>`);
  zip.file('xl/drawings/drawing2.xml', `${HEAD}<xdr:wsDr xmlns:xdr="${XDR}" xmlns:a="${A}">${frame('Year', 'sle15', `xmlns:sle15="${SLE15}"`, 5)}</xdr:wsDr>`);
  zip.file('xl/worksheets/_rels/sheet1.xml.rels', rels(rel('rId1', `${R}/table`, '../tables/table1.xml') + rel('rId2', `${R}/drawing`, '../drawings/drawing2.xml') + rel('rId3', 'http://schemas.microsoft.com/office/2007/relationships/slicer', '../slicers/slicer2.xml')));
  zip.file('xl/worksheets/_rels/sheet2.xml.rels', rels(rel('rId1', `${R}/pivotTable`, '../pivotTables/pivotTable1.xml') + rel('rId2', `${R}/drawing`, '../drawings/drawing1.xml') + rel('rId3', 'http://schemas.microsoft.com/office/2007/relationships/slicer', '../slicers/slicer1.xml')));
  zip.file('xl/worksheets/sheet1.xml', (await text('xl/worksheets/sheet1.xml')).replace('</worksheet>', `<drawing r:id="rId2"/><tableParts count="1"><tablePart r:id="rId1"/></tableParts><extLst><ext xmlns:x15="${X15}" uri="{3A4CF648-6AED-40f4-86FF-DC5316D8AED3}"><x14:slicerList xmlns:x14="${X14}"><x14:slicer r:id="rId3"/></x14:slicerList></ext></extLst></worksheet>`));
  zip.file('xl/worksheets/sheet2.xml', (await text('xl/worksheets/sheet2.xml')).replace('</worksheet>', `<drawing r:id="rId2"/><extLst><ext xmlns:x14="${X14}" uri="{A8765BA9-456A-4dab-B4F3-ACF838C121DE}"><x14:slicerList><x14:slicer r:id="rId3"/></x14:slicerList></ext></extLst></worksheet>`));
  let book = await text('xl/workbook.xml');
  book = book.replace(/sheetId="1"/, 'sheetId="4"').replace(/sheetId="2"/, 'sheetId="9"');
  book = book.replace('</workbook>', `<pivotCaches><pivotCache cacheId="11" r:id="rIdP"/></pivotCaches><extLst><ext uri="{BBE1A952-AA13-448e-AADC-164F8A28A991}" xmlns:x14="${X14}"><x14:slicerCaches><x14:slicerCache r:id="rIdS1"/></x14:slicerCaches></ext><ext uri="{46BE6895-7355-4a93-B00E-2C351335B9C9}" xmlns:x15="${X15}"><x15:slicerCaches xmlns:x14="${X14}"><x14:slicerCache r:id="rIdS2"/></x15:slicerCaches></ext></extLst></workbook>`);
  if (!book.includes('<definedNames>')) book = book.replace('<calcPr', '<definedNames><definedName name="Slicer_Region">#N/A</definedName><definedName name="Slicer_Year">#N/A</definedName></definedNames><calcPr');
  zip.file('xl/workbook.xml', book);
  zip.file('xl/_rels/workbook.xml.rels', (await text('xl/_rels/workbook.xml.rels')).replace('</Relationships>', rel('rIdP', `${R}/pivotCacheDefinition`, 'pivotCache/pivotCacheDefinition1.xml') + rel('rIdS1', 'http://schemas.microsoft.com/office/2007/relationships/slicerCache', 'slicerCaches/slicerCache1.xml') + rel('rIdS2', 'http://schemas.microsoft.com/office/2007/relationships/slicerCache', 'slicerCaches/slicerCache2.xml') + '</Relationships>'));
  const over = (p: string, t: string) => `<Override PartName="/${p}" ContentType="${t}"/>`;
  zip.file(
    '[Content_Types].xml',
    (await text('[Content_Types].xml')).replace(
      '</Types>',
      over('xl/pivotCache/pivotCacheDefinition1.xml', 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheDefinition+xml') +
        over('xl/pivotCache/pivotCacheRecords1.xml', 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotCacheRecords+xml') +
        over('xl/pivotTables/pivotTable1.xml', 'application/vnd.openxmlformats-officedocument.spreadsheetml.pivotTable+xml') +
        over('xl/tables/table1.xml', 'application/vnd.openxmlformats-officedocument.spreadsheetml.table+xml') +
        over('xl/slicers/slicer1.xml', 'application/vnd.ms-excel.slicer+xml') +
        over('xl/slicers/slicer2.xml', 'application/vnd.ms-excel.slicer+xml') +
        over('xl/slicerCaches/slicerCache1.xml', 'application/vnd.ms-excel.slicerCache+xml') +
        over('xl/slicerCaches/slicerCache2.xml', 'application/vnd.ms-excel.slicerCache+xml') +
        over('xl/drawings/drawing1.xml', 'application/vnd.openxmlformats-officedocument.drawing+xml') +
        over('xl/drawings/drawing2.xml', 'application/vnd.openxmlformats-officedocument.drawing+xml') +
        '</Types>',
    ),
  );
  return zip.generateAsync({ type: 'arraybuffer' });
}

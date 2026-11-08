import ExcelJS from 'exceljs';
import JSZip from 'jszip';

import { RESOURCE_CONDITIONAL_FORMATTING, RESOURCE_DATA_VALIDATION, RESOURCE_NOTES, UniverCfRule, UniverDataValidationRule, UniverWorkbook, readResource, writeResource } from './univer-sheet-types';
import { workbookToXlsx } from './xlsx-export';
import { xlsxToWorkbook } from './xlsx-import';
import { RESOURCE_THREAD_COMMENTS, UniverComment } from './xlsx-threaded-comments';

const MAIN = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
const X14 = 'http://schemas.microsoft.com/office/spreadsheetml/2009/9/main';
const XM = 'http://schemas.microsoft.com/office/excel/2006/main';
const GUID_A = '{11111111-1111-4111-8111-111111111111}';
const GUID_B = '{22222222-2222-4222-8222-222222222222}';
const PERSON = '{33333333-3333-4333-8333-333333333333}';

/** A workbook with rules written into its sheet XML, as Excel writes them. */
async function fixture(opts: { rules?: string; ext?: string; dxfs?: string; touch?: (zip: JSZip) => void | Promise<void>; two?: boolean } = {}): Promise<ArrayBuffer> {
  const wb = new ExcelJS.Workbook();
  const ws = wb.addWorksheet('Data');
  ws.getCell('A1').value = 5;
  ws.getCell('A2').value = 10;
  wb.addWorksheet('Lists').getCell('A1').value = 'x';
  const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
  const path = 'xl/worksheets/sheet1.xml';
  let sheet = await zip.file(path)!.async('string');
  sheet = sheet.replace('<pageMargins', `${opts.rules ?? ''}<pageMargins`);
  if (opts.ext) sheet = sheet.replace('</worksheet>', `<extLst>${opts.ext}</extLst></worksheet>`);
  zip.file(path, sheet);
  if (opts.dxfs) {
    const styles = await zip.file('xl/styles.xml')!.async('string');
    zip.file('xl/styles.xml', styles.replace(/<dxfs[^>]*\/>/, `<dxfs count="${(opts.dxfs.match(/<dxf>/g) ?? []).length}">${opts.dxfs}</dxfs>`));
  }
  await opts.touch?.(zip);
  return zip.generateAsync({ type: 'arraybuffer' });
}

const open = (bytes: ArrayBuffer | Uint8Array) => xlsxToWorkbook(bytes, { id: 'wb', name: 'f.xlsx' });
const cfOf = (wb: UniverWorkbook) => readResource<Record<string, UniverCfRule[]>>(wb, RESOURCE_CONDITIONAL_FORMATTING)![wb.sheetOrder[0]];
const dvOf = (wb: UniverWorkbook) => readResource<Record<string, UniverDataValidationRule[]>>(wb, RESOURCE_DATA_VALIDATION)![wb.sheetOrder[0]];

async function sheetXml(bytes: Uint8Array): Promise<string> {
  return (await JSZip.loadAsync(bytes)).file('xl/worksheets/sheet1.xml')!.async('string');
}

/** Saves and reads back, the check every rule has to pass. */
async function roundTrip(bytes: ArrayBuffer) {
  const first = await open(bytes);
  const saved = await workbookToXlsx(first.workbook, first.preserved);
  const second = await open(saved.bytes);
  await new ExcelJS.Workbook().xlsx.load(saved.bytes as never);
  return { first, saved, second };
}

const DATA_BAR = `<conditionalFormatting sqref="A1:A9"><cfRule type="dataBar" priority="2"><dataBar showValue="0"><cfvo type="min"/><cfvo type="max"/><color rgb="FF638EC6"/></dataBar><extLst><ext xmlns:x14="${X14}" uri="{B025F937-C7B1-47D3-B67F-A62EFF666E3E}"><x14:id>${GUID_A}</x14:id></ext></extLst></cfRule></conditionalFormatting>`;
const DATA_BAR_EXT = `<ext xmlns:x14="${X14}" uri="{78C0D931-6437-407d-A8EE-F0AAD7539E65}"><x14:conditionalFormattings><x14:conditionalFormatting xmlns:xm="${XM}"><x14:cfRule type="dataBar" id="${GUID_A}"><x14:dataBar minLength="5" maxLength="90" gradient="0" direction="rightToLeft" axisPosition="middle"><x14:cfvo type="autoMin"/><x14:cfvo type="num"><xm:f>50</xm:f></x14:cfvo><x14:borderColor rgb="FF111111"/><x14:negativeFillColor rgb="FFAA0000"/><x14:negativeBorderColor rgb="FF220000"/><x14:axisColor rgb="FF333333"/></x14:dataBar></x14:cfRule><xm:sqref>A1:A9</xm:sqref></x14:conditionalFormatting></x14:conditionalFormattings></ext>`;

describe('conditional formatting', () => {
  it('merges a data bar with its x14 half into one rule, and keeps the settings Univer has no field for', async () => {
    const { first, saved, second } = await roundTrip(await fixture({ rules: DATA_BAR, ext: DATA_BAR_EXT }));
    const rules = cfOf(first.workbook);
    expect(rules).toHaveLength(1);
    expect(rules[0].rule).toEqual({
      type: 'dataBar',
      isShowValue: false,
      config: { min: { type: 'min' }, max: { type: 'num', value: 50 }, isGradient: false, positiveColor: '#638EC6', nativeColor: '#AA0000' },
    });
    const xml = await sheetXml(saved.bytes);
    const id = /<x14:id>([^<]+)<\/x14:id>/.exec(xml)![1];
    expect(xml).toContain(`<x14:cfRule type="dataBar" id="${id}">`);
    for (const piece of ['minLength="5"', 'maxLength="90"', 'gradient="0"', 'direction="rightToLeft"', 'axisPosition="middle"', 'type="autoMin"', '<x14:borderColor rgb="FF111111"/>', '<x14:negativeBorderColor rgb="FF220000"/>', '<x14:axisColor rgb="FF333333"/>']) {
      expect(xml).toContain(piece);
    }
    expect(cfOf(second.workbook)[0].rule).toEqual(rules[0].rule);
  });

  it('orders regular and x14 rules together by priority, and writes priorities from that order', async () => {
    const rules =
      '<conditionalFormatting sqref="A1"><cfRule type="expression" dxfId="0" priority="3"><formula>A1&gt;1</formula></cfRule><cfRule type="cellIs" dxfId="0" priority="1" stopIfTrue="1" operator="greaterThan"><formula>5</formula></cfRule></conditionalFormatting>';
    const ext = `<ext xmlns:x14="${X14}" uri="{78C0D931-6437-407d-A8EE-F0AAD7539E65}"><x14:conditionalFormattings><x14:conditionalFormatting xmlns:xm="${XM}"><x14:cfRule type="expression" priority="2" id="${GUID_B}"><xm:f>Lists!$A$1=A1</xm:f><x14:dxf><font><b/></font></x14:dxf></x14:cfRule><xm:sqref>A1:A2</xm:sqref></x14:conditionalFormatting></x14:conditionalFormattings></ext>`;
    const dxfs = '<dxf><fill><patternFill><bgColor rgb="FFFFC7CE"/></patternFill></fill></dxf>';
    const { first, saved, second } = await roundTrip(await fixture({ rules, ext, dxfs }));
    const list = cfOf(first.workbook);
    expect(list.map(r => (r.rule as { subType: string }).subType)).toEqual(['number', 'formula', 'formula']);
    expect(list[0].stopIfTrue).toBe(true);
    expect(list[0].rule).toMatchObject({ style: { bg: { rgb: '#FFC7CE' } } });
    expect(list[1].rule).toMatchObject({ value: '=Lists!$A$1=A1', style: { bl: 1 } });
    const xml = await sheetXml(saved.bytes);
    expect(xml).toMatch(/<cfRule type="cellIs"[^>]*priority="1" stopIfTrue="1"/);
    expect(xml).toMatch(/<x14:cfRule type="expression" priority="2"/);
    expect(xml).toMatch(/<cfRule type="expression"[^>]*priority="3"/);
    expect(cfOf(second.workbook).map(r => r.rule)).toEqual(list.map(r => r.rule));
  });

  it('reads and writes every text variant, blanks and errors', async () => {
    const dxf = '<dxf><font><color rgb="FF9C0006"/></font></dxf>';
    const rule = (type: string, attrs: string, f: string, p: number) => `<cfRule type="${type}" dxfId="0" priority="${p}"${attrs}><formula>${f}</formula></cfRule>`;
    const rules =
      '<conditionalFormatting sqref="A1:A2">' +
      rule('containsText', ' operator="containsText" text="a"', 'NOT(ISERROR(SEARCH("a",A1)))', 1) +
      rule('notContainsText', ' operator="notContains" text="b"', 'ISERROR(SEARCH("b",A1))', 2) +
      rule('beginsWith', ' operator="beginsWith" text="c"', 'LEFT(A1,LEN("c"))="c"', 3) +
      rule('endsWith', ' operator="endsWith" text="d"', 'RIGHT(A1,LEN("d"))="d"', 4) +
      rule('containsBlanks', '', 'LEN(TRIM(A1))=0', 5) +
      rule('notContainsBlanks', '', 'LEN(TRIM(A1))>0', 6) +
      rule('containsErrors', '', 'ISERROR(A1)', 7) +
      rule('notContainsErrors', '', 'NOT(ISERROR(A1))', 8) +
      rule('cellIs', ' operator="equal"', '"e"', 9) +
      '</conditionalFormatting>';
    const { first, saved, second } = await roundTrip(await fixture({ rules, dxfs: dxf }));
    expect(cfOf(first.workbook).map(r => (r.rule as { operator: string }).operator)).toEqual([
      'containsText',
      'notContainsText',
      'beginsWith',
      'endsWith',
      'containsBlanks',
      'notContainsBlanks',
      'containsErrors',
      'notContainsErrors',
      'equal',
    ]);
    expect(cfOf(first.workbook)[8].rule).toMatchObject({ subType: 'text', value: 'e' });
    const xml = await sheetXml(saved.bytes);
    expect(xml).toContain('operator="notContains" text="b"');
    expect(xml).toContain('<formula>ISERROR(SEARCH(&quot;b&quot;,A1))</formula>');
    expect(cfOf(second.workbook).map(r => r.rule)).toEqual(cfOf(first.workbook).map(r => r.rule));
  });

  it('carries time periods, ranks, averages, duplicates and uniques', async () => {
    const dxf = '<dxf><font><b/></font></dxf>';
    const rules =
      '<conditionalFormatting sqref="A1:A2">' +
      '<cfRule type="timePeriod" dxfId="0" priority="1" timePeriod="lastWeek"><formula>x</formula></cfRule>' +
      '<cfRule type="top10" dxfId="0" priority="2" percent="1" bottom="1" rank="20"/>' +
      '<cfRule type="aboveAverage" dxfId="0" priority="3" aboveAverage="0" equalAverage="1"/>' +
      '<cfRule type="duplicateValues" dxfId="0" priority="4"/>' +
      '<cfRule type="uniqueValues" dxfId="0" priority="5"/>' +
      '<cfRule type="aboveAverage" dxfId="0" priority="6" stdDev="2"/>' +
      '</conditionalFormatting>';
    const { first, saved, second } = await roundTrip(await fixture({ rules, dxfs: dxf }));
    const list = cfOf(first.workbook).map(r => r.rule);
    expect(list[0]).toMatchObject({ subType: 'timePeriod', operator: 'lastWeek' });
    expect(list[1]).toMatchObject({ subType: 'rank', isBottom: true, isPercent: true, value: 20 });
    expect(list[2]).toMatchObject({ subType: 'average', operator: 'lessThanOrEqual' });
    expect(list[3]).toMatchObject({ subType: 'duplicateValues' });
    expect(list[4]).toMatchObject({ subType: 'uniqueValues' });
    expect(list[5]).toMatchObject({ subType: 'formula', value: '=A1>AVERAGE($A$1:$A$2)+2*STDEV($A$1:$A$2)' });
    const xml = await sheetXml(saved.bytes);
    expect(xml).toMatch(/timePeriod="lastWeek"><formula>[^<]*WEEKDAY/);
    expect(cfOf(second.workbook).map(r => r.rule)).toEqual(list);
  });

  it('keeps a cellIs against a formula as a formula rule that means the same', async () => {
    const dxf = '<dxf><font><b/></font></dxf>';
    const rules =
      '<conditionalFormatting sqref="B2:B3"><cfRule type="cellIs" dxfId="0" priority="1" operator="between"><formula>$C$1</formula><formula>$C$2</formula></cfRule><cfRule type="cellIs" dxfId="0" priority="2" operator="greaterThan"><formula>$C$1</formula></cfRule></conditionalFormatting>';
    const { first } = await roundTrip(await fixture({ rules, dxfs: dxf }));
    expect(cfOf(first.workbook)[0].rule).toMatchObject({ subType: 'formula', value: '=AND(B2>=$C$1,B2<=$C$2)' });
    expect(cfOf(first.workbook)[1].rule).toMatchObject({ subType: 'formula', value: '=B2>$C$1' });
  });

  it('resolves theme colours in colour scales and carries their thresholds', async () => {
    const rules =
      '<conditionalFormatting sqref="A1:A2"><cfRule type="colorScale" priority="1"><colorScale><cfvo type="min"/><cfvo type="percentile" val="50"/><cfvo type="formula" val="$C$1"/><color rgb="FFF8696B"/><color theme="0"/><color rgb="FF63BE7B"/></colorScale></cfRule></conditionalFormatting>';
    const { first, saved, second } = await roundTrip(await fixture({ rules }));
    const rule = cfOf(first.workbook)[0].rule;
    expect(rule).toMatchObject({ type: 'colorScale' });
    expect((rule as { config: { color: string; value: unknown }[] }).config.map(c => c.value)).toEqual([{ type: 'min' }, { type: 'percentile', value: 50 }, { type: 'formula', value: '=$C$1' }]);
    expect((rule as { config: { color: string }[] }).config[1].color).toBe('#FFFFFF');
    expect(await sheetXml(saved.bytes)).toContain('<cfvo type="formula" val="$C$1"/>');
    expect(cfOf(second.workbook)[0].rule).toEqual(rule);
  });

  describe('icon sets', () => {
    const iconRule = (attrs: string) =>
      `<conditionalFormatting sqref="A1:A2"><cfRule type="iconSet" priority="1"><iconSet iconSet="3Arrows"${attrs}><cfvo type="percent" val="0"/><cfvo type="percent" val="33"/><cfvo type="num" val="67" gte="0"/></iconSet></cfRule></conditionalFormatting>`;

    it('turns Excel\'s ascending thresholds into Univer\'s top-down list', async () => {
      const { first, saved, second } = await roundTrip(await fixture({ rules: iconRule(' showValue="0"') }));
      const rule = cfOf(first.workbook)[0].rule as Extract<UniverCfRule['rule'], { type: 'iconSet' }>;
      expect(rule.isShowValue).toBe(false);
      expect(rule.config).toEqual([
        { operator: 'greaterThan', value: { type: 'num', value: 67 }, iconType: '3Arrows', iconId: '0' },
        { operator: 'greaterThanOrEqual', value: { type: 'percent', value: 33 }, iconType: '3Arrows', iconId: '1' },
        { operator: 'lessThan', value: { type: 'percent', value: 33 }, iconType: '3Arrows', iconId: '2' },
      ]);
      expect(await sheetXml(saved.bytes)).toContain('<iconSet iconSet="3Arrows" showValue="0"><cfvo type="percent" val="0"/><cfvo type="percent" val="33"/><cfvo type="num" val="67" gte="0"/></iconSet>');
      expect(cfOf(second.workbook)[0].rule).toEqual(rule);
    });

    it('keeps reverse order', async () => {
      const { first, saved } = await roundTrip(await fixture({ rules: iconRule(' reverse="1"') }));
      expect((cfOf(first.workbook)[0].rule as { config: { iconId: string }[] }).config.map(c => c.iconId)).toEqual(['2', '1', '0']);
      expect(await sheetXml(saved.bytes)).toContain('<iconSet iconSet="3Arrows" reverse="1">');
    });

    it('writes x14-only sets and custom icon lists to the extension', async () => {
      const ext = `<ext xmlns:x14="${X14}" uri="{78C0D931-6437-407d-A8EE-F0AAD7539E65}"><x14:conditionalFormattings><x14:conditionalFormatting xmlns:xm="${XM}"><x14:cfRule type="iconSet" priority="1" id="${GUID_A}"><x14:iconSet iconSet="3Stars" custom="1"><x14:cfvo type="percent"><xm:f>0</xm:f></x14:cfvo><x14:cfvo type="percent"><xm:f>33</xm:f></x14:cfvo><x14:cfvo type="percent"><xm:f>67</xm:f></x14:cfvo><x14:cfIcon iconSet="3Flags" iconId="0"/><x14:cfIcon iconSet="NoIcons" iconId="0"/><x14:cfIcon iconSet="3Stars" iconId="2"/></x14:iconSet></x14:cfRule><xm:sqref>A1:A2</xm:sqref></x14:conditionalFormatting></x14:conditionalFormattings></ext>`;
      const { first, saved, second } = await roundTrip(await fixture({ ext }));
      const rule = cfOf(first.workbook)[0].rule as { config: { iconType: string; iconId: string }[] };
      expect(rule.config.map(c => [c.iconType, c.iconId])).toEqual([
        ['3Stars', '0'],
        ['EMPTY_ICON_TYPE', '0'],
        ['3Flags', '2'],
      ]);
      const xml = await sheetXml(saved.bytes);
      expect(xml).toContain('custom="1"');
      expect(xml).toContain('<x14:cfIcon iconSet="3Flags" iconId="0"/><x14:cfIcon iconSet="NoIcons" iconId="0"/><x14:cfIcon iconSet="3Stars" iconId="2"/>');
      expect((cfOf(second.workbook)[0].rule as typeof rule).config).toEqual(rule.config);
      const plain = await fixture({ ext: ext.replace(' custom="1"', '').replace(/<x14:cfIcon[^>]*\/>/g, '') });
      const stars = await roundTrip(plain);
      expect(cfOf(stars.first.workbook)[0].rule).toMatchObject({ type: 'iconSet' });
      expect(await sheetXml(stars.saved.bytes)).toContain('<x14:iconSet iconSet="3Stars">');
    });
  });

  it('writes a rule added in Univer, with no record of its origin, and a shared style only once', async () => {
    const first = await open(await fixture());
    const sheetId = first.workbook.sheetOrder[0];
    const style = { bg: { rgb: '#FFEB9C' } };
    writeResource(first.workbook, RESOURCE_CONDITIONAL_FORMATTING, {
      [sheetId]: [
        { cfId: 'new-1', ranges: [{ startRow: 0, startColumn: 0, endRow: 1, endColumn: 0 }], stopIfTrue: false, rule: { type: 'highlightCell', subType: 'number', operator: 'greaterThan', value: 1, style } },
        { cfId: 'new-2', ranges: [{ startRow: 0, startColumn: 0, endRow: 1, endColumn: 0 }], stopIfTrue: false, rule: { type: 'highlightCell', subType: 'number', operator: 'lessThan', value: 0, style } },
      ],
    });
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const styles = await (await JSZip.loadAsync(saved.bytes)).file('xl/styles.xml')!.async('string');
    expect(styles).toMatch(/<dxfs count="1">/);
    const back = await new ExcelJS.Workbook().xlsx.load(saved.bytes as never);
    const models = (back.getWorksheet('Data') as unknown as { conditionalFormattings: { rules: { priority: number }[] }[] }).conditionalFormattings;
    expect(models.flatMap(m => m.rules.map(r => r.priority))).toEqual([1, 2]);
  });

  it('writes the worksheet schema in order, with one extLst', async () => {
    const dv = '<dataValidations count="1"><dataValidation type="whole" sqref="A1"><formula1>0</formula1></dataValidation></dataValidations>';
    const { saved } = await roundTrip(await fixture({ rules: DATA_BAR + dv, ext: DATA_BAR_EXT }));
    const xml = await sheetXml(saved.bytes);
    expect(new DOMParser().parseFromString(xml, 'application/xml').getElementsByTagName('parsererror')).toHaveLength(0);
    const order = ['<sheetData', '<conditionalFormatting', '<dataValidations', '<pageMargins'].map(t => xml.indexOf(t));
    expect(order).toEqual([...order].sort((a, b) => a - b));
    const sheetLevel = new DOMParser().parseFromString(xml, 'application/xml').documentElement;
    expect(Array.from(sheetLevel.children).filter(c => c.localName === 'extLst')).toHaveLength(1);
    expect(Array.from(sheetLevel.children).at(-1)?.localName).toBe('extLst');
  });

  it('merges with a sparkline extension kept from the original', async () => {
    const sparklines = `<ext xmlns:x14="${X14}" uri="{05C60535-1F16-4fd2-B633-F4F36F0B64E0}"><x14:sparklineGroups xmlns:xm="${XM}"/></ext>`;
    const { saved } = await roundTrip(await fixture({ rules: DATA_BAR, ext: DATA_BAR_EXT + sparklines }));
    const xml = await sheetXml(saved.bytes);
    expect(xml).toContain('sparklineGroups');
    expect(xml).toContain('{78C0D931-6437-407d-A8EE-F0AAD7539E65}');
    expect(xml.match(/<extLst>/g)?.length).toBe(2); // the sheet's and the data bar's own
  });

  it('reads rules ExcelJS wrote, as a file from anywhere else', async () => {
    const wb = new ExcelJS.Workbook();
    const ws = wb.addWorksheet('Data');
    ws.addConditionalFormatting({ ref: 'A1:A2', rules: [{ type: 'containsText', operator: 'containsText', text: 'x', priority: 1, style: { font: { bold: true } } }] });
    const first = await open((await wb.xlsx.writeBuffer()) as ArrayBuffer);
    expect(cfOf(first.workbook)[0].rule).toMatchObject({ subType: 'text', operator: 'containsText', value: 'x', style: { bl: 1 } });
  });
});

describe('data validation', () => {
  const x14 = (inner: string) => `<ext xmlns:x14="${X14}" uri="{CCE6A557-97BC-4b89-ADB6-D9C93CAAB3DF}"><x14:dataValidations xmlns:xm="${XM}" count="1">${inner}</x14:dataValidations></ext>`;

  it('reads a cross-sheet list from x14 and writes it back there', async () => {
    const ext = x14('<x14:dataValidation type="list" allowBlank="1" showErrorMessage="1" errorStyle="warning" error="No"><x14:formula1><xm:f>Lists!$A$1:$A$9</xm:f></x14:formula1><xm:sqref>A1:A5 C1</xm:sqref></x14:dataValidation>');
    const { first, saved, second } = await roundTrip(await fixture({ ext }));
    const rule = dvOf(first.workbook)[0];
    expect(rule).toMatchObject({ type: 'list', formula1: '=Lists!$A$1:$A$9', allowBlank: true, error: 'No', errorStyle: 2 });
    expect(rule.ranges).toHaveLength(2);
    const xml = await sheetXml(saved.bytes);
    expect(xml).toContain('{CCE6A557-97BC-4b89-ADB6-D9C93CAAB3DF}');
    expect(xml).toContain('<x14:formula1><xm:f>Lists!$A$1:$A$9</xm:f></x14:formula1>');
    expect(xml).toContain('<xm:sqref>A1:A5 C1</xm:sqref>');
    expect(xml).not.toContain('<dataValidations');
    expect(dvOf(second.workbook)[0]).toMatchObject({ formula1: rule.formula1, ranges: rule.ranges, errorStyle: 2 });
  });

  it('writes a huge range as one element', { timeout: 30_000 }, async () => {
    const rules = '<dataValidations count="1"><dataValidation type="list" sqref="A1:Z100000"><formula1>"a,b"</formula1></dataValidation></dataValidations>';
    const started = Date.now();
    const { first, saved } = await roundTrip(await fixture({ rules }));
    expect(dvOf(first.workbook)[0].formula1).toBe('a,b');
    const xml = await sheetXml(saved.bytes);
    expect(xml.match(/<dataValidation /g)).toHaveLength(1);
    expect(xml).toContain('sqref="A1:Z100000"');
    expect(xml).toContain('<formula1>&quot;a,b&quot;</formula1>');
    expect(Date.now() - started).toBeLessThan(20_000);
  });

  it('tells a same-sheet reference from a literal list, and keeps a hidden arrow and a stop style', async () => {
    const rules =
      '<dataValidations count="3"><dataValidation type="list" showDropDown="1" sqref="A1"><formula1>$C$1:$C$3</formula1></dataValidation><dataValidation type="custom" sqref="A2"><formula1>A2&gt;0</formula1></dataValidation><dataValidation type="whole" operator="greaterThan" sqref="A3"><formula1>$C$1</formula1></dataValidation></dataValidations>';
    const { first, saved } = await roundTrip(await fixture({ rules }));
    const [list, custom, whole] = dvOf(first.workbook);
    expect(list).toMatchObject({ formula1: '=$C$1:$C$3', showDropDown: false, errorStyle: 1 });
    expect(custom.formula1).toBe('=A2>0');
    expect(whole).toMatchObject({ formula1: '=$C$1', operator: 'greaterThan' });
    const xml = await sheetXml(saved.bytes);
    expect(xml).toContain('<formula1>$C$1:$C$3</formula1>');
    expect(xml).toContain('showDropDown="1"');
    expect(xml).not.toContain('errorStyle');
  });

  it('keeps a prompt-only rule as it was', async () => {
    const rules = '<dataValidations count="1"><dataValidation allowBlank="1" showInputMessage="1" promptTitle="T" prompt="P" sqref="B1"/></dataValidations>';
    const { first, saved } = await roundTrip(await fixture({ rules }));
    expect(readResource(first.workbook, RESOURCE_DATA_VALIDATION)).toBeNull();
    const xml = await sheetXml(saved.bytes);
    expect(xml).toContain('prompt="P"');
    expect(xml).toContain('sqref="B1"');
  });
});

describe('threaded comments', () => {
  const persons = `<?xml version="1.0" encoding="UTF-8"?><personList xmlns="http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments" xmlns:x="${MAIN}"><person displayName="Ada Lovelace" id="${PERSON}" userId="ada@example.test" providerId="Windows Live"/></personList>`;
  const comments = `<?xml version="1.0" encoding="UTF-8"?><ThreadedComments xmlns="http://schemas.microsoft.com/office/spreadsheetml/2018/threadedcomments" xmlns:x="${MAIN}"><threadedComment ref="A1" dT="2026-01-02T03:04:05.00" personId="${PERSON}" id="${GUID_A}" done="1"><text>First @Ada</text><mentions><mention mentionpersonId="${PERSON}" mentionId="${GUID_B}" startIndex="6" length="4"/></mentions></threadedComment><threadedComment ref="A1" dT="2026-01-02T04:00:00.00" personId="${PERSON}" id="${GUID_B}" parentId="${GUID_A}"><text>A reply</text></threadedComment></ThreadedComments>`;

  async function threaded(): Promise<ArrayBuffer> {
    const wb = new ExcelJS.Workbook();
    const ws = wb.addWorksheet('Data');
    ws.getCell('A1').value = 'x';
    ws.getCell('A1').note = '[Threaded comment] placeholder';
    ws.getCell('B1').value = 'y';
    ws.getCell('B1').note = 'A real note';
    const zip = await JSZip.loadAsync(await wb.xlsx.writeBuffer());
    zip.file('xl/persons/person.xml', persons);
    zip.file('xl/threadedComments/threadedComment1.xml', comments);
    const sheetRels = await zip.file('xl/worksheets/_rels/sheet1.xml.rels')!.async('string');
    zip.file(
      'xl/worksheets/_rels/sheet1.xml.rels',
      sheetRels.replace('</Relationships>', '<Relationship Id="rIdTC" Type="http://schemas.microsoft.com/office/2017/10/relationships/threadedComment" Target="../threadedComments/threadedComment1.xml"/></Relationships>'),
    );
    const wbRels = await zip.file('xl/_rels/workbook.xml.rels')!.async('string');
    zip.file('xl/_rels/workbook.xml.rels', wbRels.replace('</Relationships>', '<Relationship Id="rIdP" Type="http://schemas.microsoft.com/office/2017/10/relationships/person" Target="persons/person.xml"/></Relationships>'));
    return zip.generateAsync({ type: 'arraybuffer' });
  }

  const threadsOf = (wb: UniverWorkbook) => readResource<Record<string, UniverComment[]>>(wb, RESOURCE_THREAD_COMMENTS)![wb.sheetOrder[0]];

  it('reads threads with their authors and replies, and leaves plain notes as notes', async () => {
    const { workbook, report } = await open(await threaded());
    const [root] = threadsOf(workbook);
    expect(root).toMatchObject({ id: GUID_A, threadId: GUID_A, ref: 'A1', personId: PERSON, authorName: 'Ada Lovelace', resolved: true, subUnitId: workbook.sheetOrder[0], unitId: 'wb' });
    expect(root.text.dataStream).toBe('First @Ada\r\n');
    expect(root.children).toHaveLength(1);
    expect(root.children![0]).toMatchObject({ id: GUID_B, parentId: GUID_A, threadId: GUID_A, authorName: 'Ada Lovelace' });
    const notes = readResource<Record<string, Record<number, Record<number, { note: string }>>>>(workbook, RESOURCE_NOTES)![workbook.sheetOrder[0]];
    expect(notes[0][1].note).toBe('A real note');
    expect(notes[0][0]).toBeUndefined();
    expect(report.warnings.map(w => w.feature)).not.toContain('Threaded comments are saved as notes');
  });

  it('writes the threads, people and legacy placeholders back, with relationships and types', async () => {
    const first = await open(await threaded());
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const zip = await JSZip.loadAsync(saved.bytes);
    const part = await zip.file('xl/threadedComments/threadedComment1.xml')!.async('string');
    expect(part).toContain(`id="${GUID_A}"`);
    expect(part).toContain(`parentId="${GUID_A}"`);
    expect(part).toContain('done="1"');
    expect(part).toContain('dT="2026-01-02T03:04:05.00"');
    expect(part).toContain('<mention mentionpersonId');
    const people = await zip.file('xl/persons/person.xml')!.async('string');
    expect(people).toContain('displayName="Ada Lovelace"');
    expect(people).toContain('userId="ada@example.test"');
    expect(people).toContain('providerId="Windows Live"');
    const types = await zip.file('[Content_Types].xml')!.async('string');
    expect(types).toContain('/xl/threadedComments/threadedComment1.xml');
    expect(types).toContain('application/vnd.ms-excel.person+xml');
    expect(await zip.file('xl/worksheets/_rels/sheet1.xml.rels')!.async('string')).toContain('threadedComment1.xml');
    expect(await zip.file('xl/_rels/workbook.xml.rels')!.async('string')).toContain('persons/person.xml');
    const legacyPath = Object.keys(zip.files).find(p => /xl\/comments\d*\.xml$/.test(p))!;
    const legacy = await zip.file(legacyPath)!.async('string');
    expect(legacy).toContain(`<author>tc=${GUID_A}</author>`);
    expect(legacy).toContain('[Threaded comment]');
    expect(legacy).toContain('A real note');
    expect(legacy).toContain(`xr:uid="${GUID_A}"`);
    expect(new DOMParser().parseFromString(legacy, 'application/xml').getElementsByTagName('parsererror')).toHaveLength(0);

    const second = await open(saved.bytes);
    expect(threadsOf(second.workbook)).toEqual(threadsOf(first.workbook));
  });

  it('gives comments made in Univer proper ids and an author, and drops deleted ones', async () => {
    const first = await open(await threaded());
    const sheetId = first.workbook.sheetOrder[0];
    const threads = threadsOf(first.workbook);
    threads[0].children = [];
    threads.push({
      id: 'abc123',
      threadId: 'abc123',
      ref: 'B2',
      dT: '2026/02/03 10:20:30',
      personId: 'mock-user',
      text: { dataStream: 'Mine\r\n', textRuns: [], paragraphs: [{ startIndex: 4 }] },
      unitId: 'wb',
      subUnitId: sheetId,
      children: [
        { id: 'def456', threadId: 'abc123', parentId: 'abc123', ref: 'B2', dT: '2026/02/03 10:21:00', personId: 'mock-user', text: { dataStream: 'Re\r\n', textRuns: [], paragraphs: [{ startIndex: 2 }] }, unitId: 'wb', subUnitId: sheetId },
      ],
    });
    writeResource(first.workbook, RESOURCE_THREAD_COMMENTS, { [sheetId]: threads });
    const saved = await workbookToXlsx(first.workbook, first.preserved);
    const zip = await JSZip.loadAsync(saved.bytes);
    const part = await zip.file('xl/threadedComments/threadedComment1.xml')!.async('string');
    expect(part).not.toContain('A reply');
    expect(part).not.toContain('abc123');
    expect(part.match(/ id="\{[0-9A-F-]{36}\}"/g)).toHaveLength(3);
    const newId = /ref="B2"[^>]* id="(\{[^"]+\})" done="0"/.exec(part)![1];
    expect(part).toContain(`parentId="${newId}"`);
    expect(part).toContain('dT="2026-02-03T10:20:30.00"');
    const people = await zip.file('xl/persons/person.xml')!.async('string');
    expect(people).toContain('displayName="WhatIff user"');
    expect(people).toContain('displayName="Ada Lovelace"');
  });
});

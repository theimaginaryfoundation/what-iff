import { OoxmlPackage } from '../shared/ooxml';
import { documentToDocx } from './docx-export';
import { docxToDocument } from './docx-import';
import { DocDocument, TOKEN } from './univer-doc-types';

const W = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main';
const W14 = 'http://schemas.microsoft.com/office/word/2010/wordml';
const PKG = 'http://schemas.openxmlformats.org/package/2006/relationships';
const CT = 'http://schemas.openxmlformats.org/package/2006/content-types';
const OFFICE = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>';

/** A package around a document body, with Heading1 and Normal styles. */
async function packageOf(body: string, settings = ''): Promise<Uint8Array> {
  const pkg = OoxmlPackage.empty();
  pkg.put(
    '[Content_Types].xml',
    `${HEAD}<Types xmlns="${CT}"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>` +
      `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
      `<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
      `<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/></Types>`,
  );
  pkg.put('_rels/.rels', `${HEAD}<Relationships xmlns="${PKG}"><Relationship Id="rId1" Type="${OFFICE}/officeDocument" Target="word/document.xml"/></Relationships>`);
  pkg.put(
    'word/_rels/document.xml.rels',
    `${HEAD}<Relationships xmlns="${PKG}"><Relationship Id="rId1" Type="${OFFICE}/styles" Target="styles.xml"/><Relationship Id="rId2" Type="${OFFICE}/settings" Target="settings.xml"/></Relationships>`,
  );
  pkg.put(
    'word/styles.xml',
    `${HEAD}<w:styles xmlns:w="${W}"><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
      `<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:rPr><w:b/></w:rPr></w:style></w:styles>`,
  );
  pkg.put('word/settings.xml', `${HEAD}<w:settings xmlns:w="${W}">${settings}</w:settings>`);
  pkg.put(
    'word/document.xml',
    `${HEAD}<w:document xmlns:w="${W}" xmlns:w14="${W14}"><w:body>${body}<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:body></w:document>`,
  );
  return pkg.save('application/zip');
}

/** Imports, applies an edit, exports over the original, and gives the document part and its re-import. */
async function roundTrip(bytes: Uint8Array, edit?: (doc: DocDocument) => void) {
  const { document, report } = await docxToDocument(bytes, { id: 'd', name: 'a.docx' });
  edit?.(document);
  const saved = await documentToDocx(document, bytes);
  const pkg = await OoxmlPackage.open(saved.bytes);
  const xml = (await pkg.text('word/document.xml')) ?? '';
  const again = await docxToDocument(saved.bytes, { id: 'd', name: 'a.docx' });
  return { document, report, xml, pkg, again: again.document, savedBytes: saved.bytes };
}

/** An edit that inserts text at the start of the document, moving everything after it. */
function typeAtStart(doc: DocDocument, text = 'New '): void {
  const n = text.length;
  const b = doc.body;
  b.dataStream = text + b.dataStream;
  for (const r of b.textRuns ?? []) {
    r.st += n;
    r.ed += n;
  }
  for (const p of b.paragraphs ?? []) p.startIndex += n;
  for (const s of b.sectionBreaks ?? []) s.startIndex += n;
  for (const c of [...(b.customBlocks ?? []), ...(b.docxRawCustomBlocks ?? []), ...(b.docxRawBlocks ?? [])]) c.startIndex += n;
  for (const r of b.customRanges ?? []) {
    r.startIndex += n;
    r.endIndex += n;
  }
  for (const t of b.tables ?? []) {
    t.startIndex += n;
    t.endIndex += n;
  }
}

const P = (inner: string) => `<w:p>${inner}</w:p>`;
const T = (text: string) => `<w:r><w:t xml:space="preserve">${text}</w:t></w:r>`;

describe('soft line breaks', () => {
  it('stay in one paragraph, as a bare vertical tab, and go back as w:br', async () => {
    const { document, report, xml } = await roundTrip(await packageOf(P(`${T('one')}<w:r><w:br/></w:r>${T('two')}`)));
    expect(document.body.paragraphs).toHaveLength(1);
    expect(document.body.dataStream.startsWith(`one${TOKEN.COLUMN_BREAK}two\r`)).toBe(true);
    expect(document.body.customRanges ?? []).toEqual([]);
    expect(report.warnings.map(w => w.feature)).not.toContain('Line breaks became paragraphs');
    expect(xml).toContain('<w:br/>');
    expect(xml).not.toContain('w:type="column"');
    expect(xml.match(/<w:p>/g)).toHaveLength(1);
  });

  it('keep w:cr and a clearing break as they were', async () => {
    const { document, xml } = await roundTrip(await packageOf(P(`${T('a')}<w:r><w:cr/></w:r>${T('b')}<w:r><w:br w:clear="all"/></w:r>${T('c')}`)));
    expect(document.body.paragraphs).toHaveLength(1);
    expect(xml).toContain('<w:cr/>');
    expect(xml).toContain('<w:br w:clear="all"/>');
  });

  it('are not column breaks, which Univer needs marked', async () => {
    const { document, xml } = await roundTrip(await packageOf(P(`${T('a')}<w:r><w:br w:type="column"/></w:r>${T('b')}`)));
    expect(document.body.customRanges?.[0].properties?.['breakType']).toBe('column');
    expect(document.body.customRanges?.[0].wholeEntity).toBe(true);
    expect(xml).toContain('<w:br w:type="column"/>');
  });

  it('leave page breaks alone', async () => {
    const { xml } = await roundTrip(await packageOf(P(`${T('a')}<w:r><w:br w:type="page"/></w:r>${T('b')}`)));
    expect(xml).toContain('<w:br w:type="page"/>');
  });
});

describe('content controls', () => {
  const CHECKBOX = `<w:sdtPr><w:alias w:val="Agree"/><w:tag w:val="agree"/><w:id w:val="11"/><w14:checkbox><w14:checked w14:val="1"/><w14:checkedState w14:val="2612" w14:font="MS Gothic"/><w14:uncheckedState w14:val="2610" w14:font="MS Gothic"/></w14:checkbox></w:sdtPr>`;
  const DROPDOWN = `<w:sdtPr><w:id w:val="12"/><w:dropDownList><w:listItem w:displayText="Red" w:value="r"/><w:listItem w:displayText="Blue" w:value="b"/></w:dropDownList></w:sdtPr>`;
  const DATE = `<w:sdtPr><w:id w:val="13"/><w:date w:fullDate="2026-11-07T00:00:00Z"><w:dateFormat w:val="M/d/yyyy"/><w:lid w:val="en-US"/><w:storeMappedDataAs w:val="dateTime"/><w:calendar w:val="gregorian"/></w:date></w:sdtPr>`;
  const sdt = (pr: string, content: string, endPr = '') => `<w:sdt>${pr}${endPr}<w:sdtContent>${content}</w:sdtContent></w:sdt>`;
  /** The serialiser may declare w14 on the fragment; the markup itself must come back the same. */
  const sameMarkup = (xml: string, fragment: string) => expect(xml.replace(/ xmlns:w14="[^"]*"/g, '')).toContain(fragment);

  it('keep run-level properties through an edit elsewhere, around the same text', async () => {
    const body = P(`${T('Before ')}${sdt(CHECKBOX, T('☒'))}${T(' mid ')}${sdt(DROPDOWN, T('Red'))}${T(' on ')}${sdt(DATE, T('11/7/2026'))}`);
    const { document, report, xml, again } = await roundTrip(await packageOf(body), typeAtStart);
    const controls = document.body.customRanges!.filter(r => r.rangeType === 2);
    expect(controls.map(c => c.properties?.['kind'])).toEqual(['checkbox', 'dropDownList', 'date']);
    expect(controls[0].properties?.['alias']).toBe('Agree');
    expect(report.warnings.map(w => w.feature)).not.toContain('Content controls kept as plain content');
    sameMarkup(xml, `<w:sdt>${CHECKBOX}<w:sdtContent><w:r><w:t xml:space="preserve">☒</w:t></w:r></w:sdtContent></w:sdt>`);
    sameMarkup(xml, `<w:sdt>${DROPDOWN}<w:sdtContent><w:r><w:t xml:space="preserve">Red</w:t></w:r></w:sdtContent></w:sdt>`);
    sameMarkup(xml, `<w:sdt>${DATE}<w:sdtContent>`);
    expect(xml).toContain('New Before');
    expect(again.body.customRanges!.filter(r => r.rangeType === 2)).toHaveLength(3);
  });

  it('keep sdtEndPr, block-level controls and multi-paragraph spans', async () => {
    const pr = '<w:sdtPr><w:id w:val="21"/><w:richText/></w:sdtPr>';
    const end = '<w:sdtEndPr><w:rPr><w:b/></w:rPr></w:sdtEndPr>';
    const { document, xml } = await roundTrip(await packageOf(`${P(T('Top'))}${sdt(pr, P(T('In one')) + P(T('In two')), end)}${P(T('After'))}`), typeAtStart);
    const control = document.body.customRanges!.find(r => r.rangeType === 2)!;
    expect(control.properties?.['placement']).toBe('block');
    expect(xml).toContain(`${pr}${end}<w:sdtContent><w:p>`);
    expect(xml).toMatch(/In two<\/w:t><\/w:r><\/w:p><\/w:sdtContent><\/w:sdt><w:p>/);
    expect(xml.indexOf('New Top')).toBeLessThan(xml.indexOf('<w:sdt>'));
  });

  it('keep row-level and cell-level controls', async () => {
    const row = '<w:sdtPr><w:id w:val="31"/></w:sdtPr>';
    const cell = '<w:sdtPr><w:id w:val="32"/></w:sdtPr>';
    const tc = (t: string) => `<w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr>${P(T(t))}</w:tc>`;
    const table = `<w:tbl><w:tblPr><w:tblW w:w="4000" w:type="dxa"/></w:tblPr><w:tblGrid><w:gridCol w:w="2000"/><w:gridCol w:w="2000"/></w:tblGrid>${sdt(row, `<w:tr>${tc('A')}${tc('B')}</w:tr>`)}<w:tr>${sdt(cell, tc('C'))}${tc('D')}</w:tr></w:tbl>`;
    const { document, xml } = await roundTrip(await packageOf(P(T('Top')) + table + P('')), typeAtStart);
    expect(
      document.body.customRanges!.filter(r => r.rangeType === 2).map(r => r.properties?.['placement']).sort(),
    ).toEqual(['cell', 'row']);
    expect(xml).toMatch(new RegExp(`<w:tbl>.*<w:tblGrid>.*</w:tblGrid><w:sdt>${row}<w:sdtContent><w:tr>.*</w:tr></w:sdtContent></w:sdt><w:tr>`));
    expect(xml).toMatch(new RegExp(`<w:tr><w:sdt>${cell}<w:sdtContent><w:tc>.*C</w:t>.*</w:tc></w:sdtContent></w:sdt><w:tc>`));
  });

  it('nest: a control inside a cell, and around a hyperlink', async () => {
    const pr = '<w:sdtPr><w:id w:val="41"/></w:sdtPr>';
    const inner = sdt(pr, P(T('Cell')));
    const table = `<w:tbl><w:tblPr/><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/></w:tcPr>${inner}</w:tc></w:tr></w:tbl>`;
    const linked = sdt('<w:sdtPr><w:id w:val="42"/></w:sdtPr>', `<w:hyperlink w:anchor="x">${T('Go')}</w:hyperlink>`);
    const { xml } = await roundTrip(await packageOf(table + P(linked) + P(`<w:bookmarkStart w:id="1" w:name="x"/>${T('t')}<w:bookmarkEnd w:id="1"/>`)));
    expect(xml).toMatch(new RegExp(`<w:tc><w:tcPr>.*</w:tcPr><w:sdt>${pr}<w:sdtContent><w:p>`));
    expect(xml).toMatch(/<w:sdt><w:sdtPr><w:id w:val="42"\/><\/w:sdtPr><w:sdtContent><w:hyperlink [^>]*>.*<\/w:hyperlink><\/w:sdtContent><\/w:sdt>/);
  });

  it('keep an empty control verbatim, and let a deleted one go', async () => {
    const empty = sdt('<w:sdtPr><w:id w:val="51"/></w:sdtPr>', '');
    const first = await roundTrip(await packageOf(P(`${T('a')}${empty}`)));
    expect(first.xml).toContain('<w:id w:val="51"/>');
    const gone = await roundTrip(await packageOf(P(`${T('a')}${sdt('<w:sdtPr><w:id w:val="52"/></w:sdtPr>', T('b'))}`)), doc => {
      doc.body.customRanges = doc.body.customRanges!.filter(r => r.rangeType !== 2);
    });
    expect(gone.xml).not.toContain('w:val="52"');
    expect(gone.xml).toContain('>ab<');
  });

  it('stop being a placeholder once typed over, and stay one otherwise', async () => {
    const pr = '<w:sdtPr><w:id w:val="61"/><w:showingPlcHdr/><w:text/></w:sdtPr>';
    const body = P(sdt(pr, T('Click here')));
    const kept = await roundTrip(await packageOf(body));
    expect(kept.xml).toContain('<w:showingPlcHdr/>');
    const typed = await roundTrip(await packageOf(body), doc => {
      doc.body.dataStream = doc.body.dataStream.replace('Click here', 'Mine here!');
    });
    expect(typed.xml).toContain('Mine here!');
    expect(typed.xml).not.toContain('showingPlcHdr');
    expect(typed.xml).toContain('<w:id w:val="61"/><w:text/>');
  });

  it('split with a paragraph an edit made inside them, keeping the id on the first part only', async () => {
    const pr = '<w:sdtPr><w:id w:val="81"/><w:alias w:val="A"/></w:sdtPr>';
    const { xml } = await roundTrip(await packageOf(P(sdt(pr, T('onetwo')))), doc => {
      const b = doc.body;
      b.dataStream = b.dataStream.replace('onetwo', 'one\rtwo');
      for (const p of b.paragraphs ?? []) p.startIndex += 1;
      b.paragraphs!.unshift({ startIndex: 3, paragraphId: 'split' });
      for (const s of b.sectionBreaks ?? []) s.startIndex += 1;
      b.customRanges![0].endIndex += 1;
    });
    expect(xml.match(/<w:sdt>/g)).toHaveLength(2);
    expect(xml.match(/w:val="81"/g)).toHaveLength(1);
    expect(xml.match(/w:alias/g)).toHaveLength(2);
    expect(new DOMParser().parseFromString(xml, 'application/xml').getElementsByTagName('parsererror')).toHaveLength(0);
  });

  it('warn that a data-bound control may not keep an edit', async () => {
    const pr = '<w:sdtPr><w:dataBinding w:xpath="/a" w:storeItemID="{X}"/><w:id w:val="71"/></w:sdtPr>';
    const { report, xml } = await roundTrip(await packageOf(P(sdt(pr, T('v')))));
    expect(report.warnings.map(w => w.feature).join()).toContain('refresh from it in Word');
    expect(xml).toContain('w:storeItemID="{X}"');
  });
});

describe('tracked changes', () => {
  const INS = '<w:ins w:id="1" w:author="Ada" w:date="2026-11-01T10:00:00Z">';
  const DEL = '<w:del w:id="2" w:author="Bob" w:date="2026-11-02T10:00:00Z"><w:r><w:delText>gone</w:delText></w:r></w:del>';

  it('keep insertions and deletions with their author and date through an edit elsewhere', async () => {
    const body = P(`${T('Keep ')}${INS}${T('added')}</w:ins>${DEL}${T(' tail')}`);
    const { document, report, xml } = await roundTrip(await packageOf(body), typeAtStart);
    expect(xml).toContain(`${INS}<w:r><w:t xml:space="preserve">added</w:t></w:r></w:ins>`);
    expect(xml).toContain(DEL);
    expect(xml.indexOf('added')).toBeLessThan(xml.indexOf('gone'));
    expect(xml.indexOf('gone')).toBeLessThan(xml.indexOf(' tail'));
    expect(xml).toContain('New Keep');
    const features = report.warnings.map(w => w.feature);
    expect(features).toContain('Tracked changes are kept; they can be accepted or rejected in Word');
    expect(features).not.toContain('Tracked changes accepted');
    expect(report.dropped).toEqual([]);
    // Deleted text is not in the visible stream; inserted text is.
    expect(document.body.dataStream).toContain('added');
    expect(document.body.dataStream).not.toContain('gone');
  });

  it('mark an insertion with a range, not with run formatting that would be saved', async () => {
    const { document, xml } = await roundTrip(await packageOf(P(`${INS}${T('added')}</w:ins>`)));
    const range = document.body.customRanges!.find(r => r.rangeType === 5)!;
    expect(range.active).not.toBe(false);
    expect(document.body.textRuns ?? []).toEqual([]);
    expect(xml).not.toContain('<w:u ');
  });

  it('keep rPrChange last in the run properties, on the runs it covers', async () => {
    const change = '<w:rPrChange w:id="3" w:author="Ada" w:date="2026-11-01T10:00:00Z"><w:rPr><w:i/></w:rPr></w:rPrChange>';
    const body = P(`<w:r><w:rPr><w:b/>${change}</w:rPr><w:t>bold</w:t></w:r>${T(' plain')}`);
    const { document, xml } = await roundTrip(await packageOf(body), typeAtStart);
    expect(document.body.customRanges![0].active).toBe(false);
    expect(xml).toMatch(/<w:rPr><w:b\/><w:bCs\/><w:rPrChange [^>]*><w:rPr><w:i\/><\/w:rPr><\/w:rPrChange><\/w:rPr><w:t xml:space="preserve">bold/);
    expect(xml.match(/rPrChange w:id/g)).toHaveLength(1);
  });

  it('keep the marks on a paragraph mark, in schema order', async () => {
    const mark = '<w:ins w:id="4" w:author="Ada" w:date="2026-11-01T10:00:00Z"/>';
    const change = '<w:rPrChange w:id="5" w:author="Ada"><w:rPr/></w:rPrChange>';
    const { xml } = await roundTrip(await packageOf(P(`<w:pPr><w:rPr>${mark}<w:b/>${change}</w:rPr></w:pPr>${T('x')}`)));
    expect(xml).toMatch(/<w:pPr><w:rPr><w:ins [^>]*\/><w:b\/><w:bCs\/><w:rPrChange /);
  });

  it('keep property changes on paragraphs, cells, rows, tables and sections', async () => {
    const pPrChange = '<w:pPrChange w:id="6" w:author="A"><w:pPr/></w:pPrChange>';
    const tcPrChange = '<w:tcPrChange w:id="7" w:author="A"><w:tcPr/></w:tcPrChange>';
    const rowIns = '<w:ins w:id="8" w:author="A"/>';
    const trPrChange = '<w:trPrChange w:id="9" w:author="A"><w:trPr/></w:trPrChange>';
    const tblPrChange = '<w:tblPrChange w:id="10" w:author="A"><w:tblPr/></w:tblPrChange>';
    const sectChange = '<w:sectPrChange w:id="11" w:author="A"/>';
    const table = `<w:tbl><w:tblPr><w:tblW w:w="2000" w:type="dxa"/>${tblPrChange}</w:tblPr><w:tblGrid><w:gridCol w:w="2000"/></w:tblGrid><w:tr><w:trPr>${rowIns}${trPrChange}</w:trPr><w:tc><w:tcPr><w:tcW w:w="2000" w:type="dxa"/>${tcPrChange}</w:tcPr>${P(T('c'))}</w:tc></w:tr></w:tbl>`;
    const bytes = await packageOf(`${P(`<w:pPr><w:jc w:val="center"/>${pPrChange}</w:pPr>${T('p')}`)}${table}${P(`<w:pPr><w:sectPr><w:pgSz w:w="12240" w:h="15840"/>${sectChange}</w:sectPr></w:pPr>${T('s')}`)}`);
    const { xml } = await roundTrip(bytes, typeAtStart);
    expect(xml).toMatch(/<w:jc w:val="center"\/><w:pPrChange /);
    expect(xml).toContain(tcPrChange);
    expect(xml).toContain(`${rowIns}${trPrChange}`);
    expect(xml).toContain(tblPrChange);
    expect(xml).toContain(sectChange);
  });

  it('keep moves, their range marks and block-level deletions', async () => {
    const moveMarks = '<w:moveFromRangeStart w:id="12" w:author="A" w:name="m"/>';
    const moveFrom = '<w:moveFrom w:id="13" w:author="A"><w:r><w:t>old</w:t></w:r></w:moveFrom>';
    const moveEnd = '<w:moveFromRangeEnd w:id="12"/>';
    const moveTo = '<w:moveTo w:id="14" w:author="A">';
    const { xml } = await roundTrip(await packageOf(P(`${moveMarks}${moveFrom}${moveEnd}${moveTo}${T('new')}</w:moveTo>`)));
    expect(xml).toContain(moveMarks);
    expect(xml).toContain(moveFrom);
    expect(xml).toContain(moveEnd);
    expect(xml).toContain(`${moveTo}<w:r><w:t xml:space="preserve">new</w:t></w:r></w:moveTo>`);
  });

  it('wrap a hyperlink whole, or sit inside one, and always nest', async () => {
    const ins = '<w:ins w:id="15" w:author="A">';
    const around = await roundTrip(await packageOf(P(`${ins}<w:hyperlink w:anchor="x">${T('link')}</w:hyperlink>${T(' more')}</w:ins>`)));
    expect(around.xml).toMatch(/<w:ins [^>]*><w:hyperlink [^>]*>.*link.*<\/w:hyperlink><w:r>.* more.*<\/w:ins>/);
    const inside = await roundTrip(await packageOf(P(`<w:hyperlink w:anchor="x">${T('a')}${ins}${T('b')}</w:ins>${T('c')}</w:hyperlink>`)));
    expect(inside.xml).toMatch(/<w:hyperlink [^>]*>.*>a<.*<w:ins [^>]*>.*>b<.*<\/w:ins>.*>c<.*<\/w:hyperlink>/);
    expect(() => new DOMParser().parseFromString(around.xml, 'application/xml')).not.toThrow();
  });

  it('split an insertion that spans block-level paragraphs, one per paragraph', async () => {
    const { xml } = await roundTrip(await packageOf(`<w:ins w:id="16" w:author="A">${P(T('one'))}${P(T('two'))}</w:ins>`));
    expect(xml.match(/<w:ins /g)).toHaveLength(2);
    expect(xml).toMatch(/<w:p>(<w:pPr>.*?<\/w:pPr>)?<w:ins [^>]*>.*one.*<\/w:ins><\/w:p><w:p>/);
  });

  it('leave w:trackRevisions in settings byte for byte', async () => {
    const bytes = await packageOf(P(T('x')), '<w:trackRevisions/>');
    const { pkg } = await roundTrip(bytes, typeAtStart);
    expect(await pkg.text('word/settings.xml')).toContain('<w:trackRevisions/>');
  });

  it('write well-formed XML for a document of every kind of revision', async () => {
    const { xml } = await roundTrip(await packageOf(P(`${INS}${T('i')}</w:ins>${DEL}<w:r><w:br/></w:r>`)));
    const doc = new DOMParser().parseFromString(xml, 'application/xml');
    expect(doc.getElementsByTagName('parsererror')).toHaveLength(0);
  });
});

describe('a heading applied in the editor', () => {
  it('exports as the style the document has', async () => {
    const { xml } = await roundTrip(await packageOf(P(T('Title'))), doc => {
      doc.body.paragraphs![0].paragraphStyle = { ...(doc.body.paragraphs![0].paragraphStyle ?? {}), namedStyleType: 4 };
    });
    expect(xml).toContain('<w:pStyle w:val="Heading1"/>');
  });

  it('does not invent a style the document lacks', async () => {
    const { xml } = await roundTrip(await packageOf(P(T('Title'))), doc => {
      doc.body.paragraphs![0].paragraphStyle = { ...(doc.body.paragraphs![0].paragraphStyle ?? {}), namedStyleType: 5 };
    });
    expect(xml).not.toContain('pStyle');
  });
});

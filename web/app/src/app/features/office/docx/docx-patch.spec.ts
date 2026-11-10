import { describe, expect, it } from 'vitest';

import { OoxmlPackage, base64ToBytes } from '../shared/ooxml';
import { documentToDocx } from './docx-export';
import { docxToDocument } from './docx-import';
import { CUSTOM_RANGE, DocDocument, NOTE_REFERENCE } from './univer-doc-types';

/**
 * Patch mode: a document written over its original package keeps every part the editor does
 * not model, and the things Univer carries natively (notes, bookmarks, comments, opaque runs)
 * go back where they were.
 */

const PNG_BASE64 =
  'iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAYAAACinX6EAAAAQklEQVR42u3OMQEAAAgDoNm/9Cz4E4kwQ6ZJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZN2uUBZl0AoYzvRCEAAAAASUVORK5CYII=';

const W = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main';
const R = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const PKG = 'http://schemas.openxmlformats.org/package/2006/relationships';
const CT = 'http://schemas.openxmlformats.org/package/2006/content-types';
const ROOT_NS =
  `xmlns:w="${W}" xmlns:r="${R}" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
  `xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
  `xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ` +
  `xmlns:v="urn:schemas-microsoft-com:vml" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml" mc:Ignorable="w14"`;

const TEXT_BOX =
  `<w:r><w:rPr><w:b/></w:rPr><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0">` +
  `<wp:extent cx="914400" cy="457200"/><wp:docPr id="7" name="Text Box 7"/><a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape">` +
  `<wps:wsp><wps:cNvSpPr txBox="1"/><wps:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="914400" cy="457200"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></wps:spPr>` +
  `<wps:txbx><w:txbxContent><w:p><w:r><w:t>Inside the box</w:t></w:r></w:p></w:txbxContent></wps:txbx></wps:wsp></a:graphicData></a:graphic></wp:inline></w:drawing></mc:Choice>` +
  `<mc:Fallback><w:pict><v:shape id="box" style="width:72pt;height:36pt"><v:textbox><w:txbxContent><w:p><w:r><w:t>Inside the box</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></mc:Fallback></mc:AlternateContent></w:r>`;

const DOCUMENT_XML =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document ${ROOT_NS}><w:body>` +
  `<w:p w14:paraId="1A2B3C4D"><w:pPr><w:pStyle w:val="Heading1"/><w:tabs><w:tab w:val="left" w:pos="2880"/></w:tabs></w:pPr><w:bookmarkStart w:id="3" w:name="target"/><w:r><w:t>Target</w:t></w:r><w:bookmarkEnd w:id="3"/><w:r><w:t xml:space="preserve"> heading</w:t></w:r></w:p>` +
  `<w:p><w:r><w:t xml:space="preserve">See </w:t></w:r><w:hyperlink w:anchor="target"><w:r><w:rPr><w:rStyle w:val="Hyperlink"/></w:rPr><w:t>the target</w:t></w:r></w:hyperlink><w:r><w:t>.</w:t></w:r></w:p>` +
  `<w:p><w:r><w:t>Noted</w:t></w:r><w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteReference w:id="1"/></w:r><w:r><w:t xml:space="preserve"> text.</w:t></w:r></w:p>` +
  `<w:p><w:commentRangeStart w:id="0"/><w:r><w:t>Commented</w:t></w:r><w:commentRangeEnd w:id="0"/><w:r><w:rPr><w:rStyle w:val="CommentReference"/></w:rPr><w:commentReference w:id="0"/></w:r><w:r><w:t xml:space="preserve"> words.</w:t></w:r></w:p>` +
  `<w:p><w:r><w:t xml:space="preserve">Boxed </w:t></w:r>${TEXT_BOX}<w:r><w:t xml:space="preserve"> after.</w:t></w:r></w:p>` +
  `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="5"/></w:numPr></w:pPr><w:r><w:t>Listed</w:t></w:r></w:p>` +
  `<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/><w:tblLook w:val="04A0"/></w:tblPr><w:tblGrid><w:gridCol w:w="4000"/></w:tblGrid>` +
  `<w:tr><w:trPr><w:cnfStyle w:val="100000000000"/></w:trPr><w:tc><w:tcPr><w:tcW w:w="4000" w:type="dxa"/><w:noWrap/></w:tcPr><w:p><w:r><w:t>Cell</w:t></w:r></w:p></w:tc></w:tr></w:tbl>` +
  `<w:p><w:r><w:t>Tail</w:t></w:r></w:p>` +
  `<w:sectPr><w:headerReference w:type="default" r:id="rId7"/><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/><w:docGrid w:linePitch="360"/></w:sectPr>` +
  `</w:body></w:document>`;

const STYLES_XML =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:styles xmlns:w="${W}"><w:docDefaults><w:rPrDefault><w:rPr><w:sz w:val="22"/></w:rPr></w:rPrDefault></w:docDefaults>` +
  `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` +
  `<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>` +
  `<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>` +
  `<w:style w:type="character" w:styleId="FootnoteReference"><w:name w:val="footnote reference"/><w:rPr><w:vertAlign w:val="superscript"/></w:rPr></w:style>` +
  `<w:style w:type="character" w:styleId="CommentReference"><w:name w:val="annotation reference"/><w:rPr><w:sz w:val="16"/></w:rPr></w:style>` +
  `<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/></w:tblBorders></w:tblPr></w:style>` +
  `</w:styles>`;

const NUMBERING_XML =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:numbering xmlns:w="${W}">` +
  `<w:abstractNum w:abstractNumId="3"><w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl></w:abstractNum>` +
  `<w:num w:numId="5"><w:abstractNumId w:val="3"/></w:num></w:numbering>`;

const FOOTNOTES_XML =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:footnotes xmlns:w="${W}">` +
  `<w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:footnote>` +
  `<w:footnote w:type="continuationSeparator" w:id="0"><w:p><w:r><w:continuationSeparator/></w:r></w:p></w:footnote>` +
  `<w:footnote w:id="1"><w:p><w:pPr><w:pStyle w:val="Normal"/></w:pPr><w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteRef/></w:r><w:r><w:t xml:space="preserve"> A footnote.</w:t></w:r></w:p></w:footnote>` +
  `</w:footnotes>`;

const COMMENTS_XML =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:comments xmlns:w="${W}">` +
  `<w:comment w:id="0" w:author="Reviewer" w:date="2026-11-07T00:00:00Z" w:initials="R"><w:p><w:r><w:t>Please check.</w:t></w:r></w:p></w:comment></w:comments>`;

const HEADER_XML =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:hdr ${ROOT_NS}><w:p><w:r><w:drawing><wp:inline><wp:extent cx="609600" cy="304800"/><wp:docPr id="1" name="Logo"/>` +
  `<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="Logo"/><pic:cNvPicPr/></pic:nvPicPr>` +
  `<pic:blipFill><a:blip r:embed="rId1"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="609600" cy="304800"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic>` +
  `</a:graphicData></a:graphic></wp:inline></w:drawing></w:r><w:r><w:t>Header text</w:t></w:r></w:p></w:hdr>`;

const SETTINGS_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:settings xmlns:w="${W}"><w:defaultTabStop w:val="720"/><w:trackRevisions/><w:rsids><w:rsidRoot w:val="00AB12CD"/></w:rsids></w:settings>`;
const THEME_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" name="Office"><a:themeElements/></a:theme>`;
const FONT_TABLE_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:fonts xmlns:w="${W}"><w:font w:name="Aptos"><w:panose1 w:val="020B0004020202020204"/></w:font></w:fonts>`;
const CUSTOM_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><root xmlns="urn:test"><value>kept</value></root>`;
const CORE_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>Fixture</dc:title><dc:creator>Someone</dc:creator></cp:coreProperties>`;
const APP_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Application>Microsoft Office Word</Application></Properties>`;
const CUSTOM_PROPS_XML = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/custom-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><property fmtid="{D5CDD505-2E9C-101B-9397-08002B2CF9AE}" pid="2" name="Project"><vt:lpwstr>WhatIff</vt:lpwstr></property></Properties>`;

const rel = (id: string, type: string, target: string) =>
  `<Relationship Id="${id}" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/${type}" Target="${target}"/>`;

const DOCUMENT_RELS =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${PKG}">` +
  rel('rId1', 'styles', 'styles.xml') +
  rel('rId2', 'settings', 'settings.xml') +
  rel('rId3', 'numbering', 'numbering.xml') +
  rel('rId4', 'footnotes', 'footnotes.xml') +
  rel('rId5', 'comments', 'comments.xml') +
  rel('rId6', 'theme', 'theme/theme1.xml') +
  rel('rId7', 'header', 'header1.xml') +
  rel('rId8', 'fontTable', 'fontTable.xml') +
  rel('rId9', 'customXml', '../customXml/item1.xml') +
  `</Relationships>`;

const CONTENT_TYPES =
  `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="${CT}">` +
  `<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>` +
  `<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
  `<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>` +
  `<Override PartName="/word/settings.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.settings+xml"/>` +
  `<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>` +
  `<Override PartName="/word/footnotes.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.footnotes+xml"/>` +
  `<Override PartName="/word/comments.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.comments+xml"/>` +
  `<Override PartName="/word/header1.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.header+xml"/>` +
  `<Override PartName="/word/theme/theme1.xml" ContentType="application/vnd.openxmlformats-officedocument.theme+xml"/>` +
  `<Override PartName="/word/fontTable.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.fontTable+xml"/>` +
  `<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` +
  `<Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` +
  `<Override PartName="/docProps/custom.xml" ContentType="application/vnd.openxmlformats-officedocument.custom-properties+xml"/>` +
  `</Types>`;

/** A hand-built package with everything the model does not carry: a Word-like document in miniature. */
async function fixture(): Promise<Uint8Array> {
  const pkg = OoxmlPackage.empty();
  pkg.put('[Content_Types].xml', CONTENT_TYPES);
  pkg.put(
    '_rels/.rels',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${PKG}">` +
      rel('rId1', 'officeDocument', 'word/document.xml') +
      `<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
      rel('rId3', 'extended-properties', 'docProps/app.xml') +
      rel('rId4', 'custom-properties', 'docProps/custom.xml') +
      `</Relationships>`,
  );
  pkg.put('word/document.xml', DOCUMENT_XML);
  pkg.put('word/_rels/document.xml.rels', DOCUMENT_RELS);
  pkg.put('word/styles.xml', STYLES_XML);
  pkg.put('word/settings.xml', SETTINGS_XML);
  pkg.put('word/numbering.xml', NUMBERING_XML);
  pkg.put('word/footnotes.xml', FOOTNOTES_XML);
  pkg.put('word/comments.xml', COMMENTS_XML);
  pkg.put('word/header1.xml', HEADER_XML);
  pkg.put(
    'word/_rels/header1.xml.rels',
    `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${PKG}">${rel('rId1', 'image', 'media/image1.png')}</Relationships>`,
  );
  pkg.put('word/media/image1.png', base64ToBytes(PNG_BASE64));
  pkg.put('word/theme/theme1.xml', THEME_XML);
  pkg.put('word/fontTable.xml', FONT_TABLE_XML);
  pkg.put('customXml/item1.xml', CUSTOM_XML);
  pkg.put('docProps/core.xml', CORE_XML);
  pkg.put('docProps/app.xml', APP_XML);
  pkg.put('docProps/custom.xml', CUSTOM_PROPS_XML);
  return pkg.save('application/vnd.openxmlformats-officedocument.wordprocessingml.document');
}

const UNTOUCHED = [
  'word/styles.xml',
  'word/settings.xml',
  'word/comments.xml',
  'word/theme/theme1.xml',
  'word/fontTable.xml',
  'customXml/item1.xml',
  'docProps/core.xml',
  'docProps/app.xml',
  'docProps/custom.xml',
  'word/media/image1.png',
];

async function roundTrip(edit?: (doc: DocDocument) => void) {
  const original = await fixture();
  const first = await docxToDocument(original, { id: 'doc', name: 'fixture.docx' });
  edit?.(first.document);
  const exported = await documentToDocx(first.document, original);
  const pkg = await OoxmlPackage.open(exported.bytes);
  const second = await docxToDocument(exported.bytes, { id: 'doc', name: 'fixture.docx' });
  return { original: await OoxmlPackage.open(original), first, exported, pkg, second };
}

describe('docx patch mode', () => {
  it('reads the fixture: notes, bookmarks, comments and the text box as native or opaque content', async () => {
    const { document, report } = await docxToDocument(await fixture(), { id: 'doc', name: 'fixture.docx' });
    expect(report.dropped).toEqual([]);
    const stream = document.body.dataStream;
    expect(stream).toContain(`Noted${NOTE_REFERENCE} text.`);
    const ranges = document.body.customRanges!;
    const note = ranges.find(r => r.rangeType === CUSTOM_RANGE.FOOTNOTE)!;
    expect(stream[note.startIndex]).toBe(NOTE_REFERENCE);
    expect(note.endIndex).toBe(note.startIndex);
    expect(note.properties).toEqual({ noteId: 'fn1' });
    expect(document.notes!['fn1'].type).toBe('footnote');
    expect(document.notes!['fn1'].body.dataStream).toBe(' A footnote.\r\n');
    const bookmark = ranges.find(r => r.rangeType === CUSTOM_RANGE.BOOKMARK)!;
    expect(stream.slice(bookmark.startIndex, bookmark.endIndex + 1)).toBe('Target');
    expect(bookmark.properties).toEqual({ name: 'target' });
    const link = ranges.find(r => r.rangeType === CUSTOM_RANGE.HYPERLINK)!;
    expect(link.properties).toEqual({ url: '#target' });
    const comment = ranges.find(r => r.rangeType === CUSTOM_RANGE.COMMENT)!;
    expect(stream.slice(comment.startIndex, comment.endIndex + 1)).toBe('Commented');
    expect(comment.properties).toEqual({ commentId: '0' });
    const raw = document.body.docxRawCustomBlocks!;
    expect(raw).toHaveLength(1);
    expect(stream[raw[0].startIndex]).toBe('\b');
    expect(stream.slice(raw[0].startIndex - 6, raw[0].startIndex + 8)).toBe('Boxed \b after.');
    expect(raw[0].docxRawXml).toContain('<wps:wsp');
    expect(raw[0].docxRawXml).toContain('<v:shape');
    expect(raw[0].docxRawXml).toMatch(/^<w:r><w:rPr[^>]*><w:b/);
    expect(report.warnings.map(w => w.feature)).toContain('Kept but not shown: shape');
    expect(document.body.paragraphs![0].docxPPr).toContain('<w:tabs');
    expect(Object.values(document.tableSource!)[0].docxTblPr).toContain('<w:tblStyle w:val="TableGrid"/>');
    expect(Object.values(document.tableSource!)[0].tableRows[0].docxTrPr).toContain('cnfStyle');
    expect(Object.values(document.tableSource!)[0].tableRows[0].tableCells[0].docxTcPr).toContain('noWrap');
    expect(document.documentStyle.docxSectPr).toContain('docGrid');
    expect(Object.values(document.headers!)[0].docxPath).toBe('word/header1.xml');
  });

  it('keeps every unmodelled part byte for byte, and the original style and list ids', async () => {
    const { original, pkg, exported } = await roundTrip();
    expect(exported.report.dropped).toEqual([]);
    for (const path of UNTOUCHED) expect((await pkg.bytes(path))!, path).toEqual((await original.bytes(path))!);
    const xml = (await pkg.text('word/document.xml'))!;
    expect(xml).toContain('<w:pStyle w:val="Heading1"/>');
    expect(xml).toContain('<w:numId w:val="5"/>');
    expect(await pkg.text('word/numbering.xml')).toBe(NUMBERING_XML);
    expect(xml).toContain('<w:tabs><w:tab w:val="left" w:pos="2880"/></w:tabs>');
    expect(xml).toMatch(/<w:pStyle w:val="Heading1"\/><w:tabs>/);
    expect(xml).toContain('<w:tblStyle w:val="TableGrid"/>');
    expect(xml).toMatch(/<w:trPr><w:cnfStyle[^>]*\/><\/w:trPr>/);
    expect(xml).toMatch(/<w:tcW[^>]*\/>(?:<w:tcBorders>.*?<\/w:tcBorders>)?<w:noWrap\/>/);
    expect(xml).toMatch(/<w:docGrid w:linePitch="360"\/><\/w:sectPr>/);
    // Relationships the raw XML and the section depend on are the original ones, plus nothing broken.
    const rels = await pkg.rels('word/document.xml');
    expect(rels.get('rId7')?.target).toBe('header1.xml');
    expect(rels.get('rId9')?.target).toBe('../customXml/item1.xml');
    expect(xml).toContain('<w:headerReference w:type="default" r:id="rId7"/>');
    const types = (await pkg.text('[Content_Types].xml'))!;
    expect(types).toContain('PartName="/docProps/custom.xml"');
    expect(types.match(/PartName="\/word\/document.xml"/g)).toHaveLength(1);
  });

  it('writes the header back to its own part and does not duplicate its picture', async () => {
    const { pkg } = await roundTrip();
    expect(pkg.paths().filter(p => p.startsWith('word/media/'))).toEqual(['word/media/image1.png']);
    expect(pkg.paths().filter(p => /^word\/header\d+\.xml$/.test(p))).toEqual(['word/header1.xml']);
    const header = (await pkg.text('word/header1.xml'))!;
    expect(header).toContain('Header text');
    const relId = /r:embed="(rId\d+)"/.exec(header)![1];
    expect((await pkg.rels('word/header1.xml')).get(relId)?.target).toBe('media/image1.png');
  });

  it('keeps an opaque text box at its place after an edit elsewhere', async () => {
    const { pkg, second } = await roundTrip(doc => {
      // Same-length edit after the box: the stream offsets before it are untouched.
      doc.body.dataStream = doc.body.dataStream.replace('Tail', 'Edit');
    });
    const xml = (await pkg.text('word/document.xml'))!;
    const boxed = xml.indexOf('Boxed ');
    const box = xml.indexOf('<mc:AlternateContent');
    const after = xml.indexOf(' after.');
    expect(boxed).toBeGreaterThan(-1);
    expect(box).toBeGreaterThan(boxed);
    expect(after).toBeGreaterThan(box);
    expect(xml).toContain('<wps:wsp');
    expect(xml).toContain('<mc:Fallback><w:pict>');
    expect(xml).toContain('>Edit</w:t>');
    expect(second.document.body.docxRawCustomBlocks).toHaveLength(1);
    expect(second.document.body.dataStream).toContain('Boxed \b after.');
  });

  it('writes footnotes back as a part Word numbers, from the stories Univer edits', async () => {
    const { pkg, second } = await roundTrip(doc => {
      doc.notes!['fn1'].body.dataStream = ' A footnote, edited.\r\n';
    });
    const xml = (await pkg.text('word/document.xml'))!;
    expect(xml).toContain('<w:r><w:rPr><w:rStyle w:val="FootnoteReference"/></w:rPr><w:footnoteReference w:id="1"/></w:r>');
    expect(xml).not.toContain(NOTE_REFERENCE);
    const notes = (await pkg.text('word/footnotes.xml'))!;
    expect(notes).toContain('w:type="separator" w:id="-1"');
    expect(notes).toContain('w:type="continuationSeparator" w:id="0"');
    expect(notes).toMatch(
      /<w:footnote w:id="1"><w:p>(<w:pPr>.*?<\/w:pPr>)?<w:r><w:rPr><w:rStyle w:val="FootnoteReference"\/><\/w:rPr><w:footnoteRef\/><\/w:r>/,
    );
    expect(notes).toContain('A footnote, edited.');
    expect(second.document.notes!['fn1'].body.dataStream).toBe(' A footnote, edited.\r\n');
    const ref = second.document.body.customRanges!.find(r => r.rangeType === CUSTOM_RANGE.FOOTNOTE)!;
    expect(second.document.body.dataStream[ref.startIndex]).toBe(NOTE_REFERENCE);
  });

  it('writes bookmarks, internal links and comment ranges back around the same text', async () => {
    const { pkg, second, first } = await roundTrip();
    const xml = (await pkg.text('word/document.xml'))!;
    expect(xml).toMatch(
      /<w:bookmarkStart w:id="(\d+)" w:name="target"\/><w:r>(<w:rPr>.*?<\/w:rPr>)?<w:t xml:space="preserve">Target<\/w:t><\/w:r><w:bookmarkEnd w:id="\1"\/>/,
    );
    expect(xml).toContain('<w:hyperlink w:anchor="target">');
    expect(xml).toMatch(
      /<w:commentRangeStart w:id="0"\/><w:r>(<w:rPr>.*?<\/w:rPr>)?<w:t xml:space="preserve">Commented<\/w:t><\/w:r><w:commentRangeEnd w:id="0"\/><w:r><w:rPr><w:rStyle w:val="CommentReference"\/><\/w:rPr><w:commentReference w:id="0"\/><\/w:r>/,
    );
    expect(await pkg.text('word/comments.xml')).toBe(COMMENTS_XML);
    const strip = (doc: DocDocument) =>
      doc.body
        .customRanges!.filter(r => r.rangeType !== CUSTOM_RANGE.FIELD)
        .map(r => ({ type: r.rangeType, text: doc.body.dataStream.slice(r.startIndex, r.endIndex + 1), properties: r.properties }));
    expect(strip(second.document)).toEqual(strip(first.document));
  });

  it('appends list definitions the editor made after the original ones', async () => {
    const { pkg } = await roundTrip(doc => {
      doc.lists!['fresh'] = {
        listType: 'fresh',
        nestingLevel: [{ bulletAlignment: 1, glyphFormat: ' %1', glyphType: 0, glyphSymbol: '•', startNumber: 0 }],
      };
      doc.body.paragraphs![doc.body.paragraphs!.length - 1].bullet = { listType: 'fresh', listId: 'fresh', nestingLevel: 0 };
    });
    const numbering = (await pkg.text('word/numbering.xml'))!;
    expect(numbering.indexOf('<w:abstractNum w:abstractNumId="3">')).toBeLessThan(numbering.indexOf('<w:abstractNum w:abstractNumId="4">'));
    expect(numbering.indexOf('<w:abstractNum w:abstractNumId="4">')).toBeLessThan(numbering.indexOf('<w:num w:numId="5">'));
    expect(numbering).toContain('<w:num w:numId="6"><w:abstractNumId w:val="4"/></w:num>');
    const xml = (await pkg.text('word/document.xml'))!;
    expect(xml).toContain('<w:numId w:val="5"/>');
    expect(xml).toContain('<w:numId w:val="6"/>');
  });

  it('without the original, comments are reported and the rest still exports', async () => {
    const first = await docxToDocument(await fixture(), { id: 'doc', name: 'fixture.docx' });
    const exported = await documentToDocx(first.document);
    expect(exported.report.dropped.map(d => d.feature)).toEqual(['Comments']);
    const pkg = await OoxmlPackage.open(exported.bytes);
    const xml = (await pkg.text('word/document.xml'))!;
    expect(xml).not.toContain('commentRangeStart');
    expect(xml).toContain('<w:footnoteReference w:id="1"/>');
    expect(await pkg.text('word/footnotes.xml')).toContain('A footnote.');
    expect(xml).toContain('<wps:wsp');
    expect(xml).toContain('<w:bookmarkStart');
  });
});

describe('table borders', () => {
  const W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"';
  const cell = (extra = '') => `<w:tc><w:tcPr><w:tcW w:w="1000" w:type="dxa"/>${extra}</w:tcPr><w:p><w:r><w:t>x</w:t></w:r></w:p></w:tc>`;
  const table = (tblPr: string, rows = 2, cells = [cell(), cell()]) =>
    `<w:tbl><w:tblPr>${tblPr}</w:tblPr><w:tblGrid><w:gridCol w:w="1000"/><w:gridCol w:w="1000"/></w:tblGrid>${Array.from({ length: rows }, () => `<w:tr>${cells.join('')}</w:tr>`).join('')}</w:tbl>`;
  const line = (side: string, val = 'single', sz = 8) => `<w:${side} w:val="${val}" w:sz="${sz}" w:space="0" w:color="FF0000"/>`;
  const styleXml = `<w:styles ${W}>
    <w:style w:type="table" w:default="1" w:styleId="TableNormal"><w:name w:val="Normal Table"/></w:style>
    <w:style w:type="table" w:styleId="Base"><w:tblPr><w:tblBorders>${line('top')}${line('left')}${line('bottom')}${line('right')}${line('insideH')}${line('insideV')}</w:tblBorders></w:tblPr></w:style>
    <w:style w:type="table" w:styleId="Derived"><w:basedOn w:val="Base"/><w:tblPr><w:tblBorders>${line('insideV', 'none', 0)}</w:tblBorders></w:tblPr></w:style>
    <w:style w:type="table" w:styleId="Header"><w:tblStylePr w:type="firstRow"><w:tcPr><w:tcBorders>${line('bottom', 'double', 16)}</w:tcBorders></w:tcPr></w:tblStylePr></w:style>
  </w:styles>`;
  async function open(body: string) {
    const pkg = OoxmlPackage.empty();
    pkg.put('_rels/.rels', `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`);
    pkg.put('word/_rels/document.xml.rels', `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`);
    pkg.put('word/styles.xml', styleXml);
    pkg.put('[Content_Types].xml', `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`);
    pkg.put('word/document.xml', `<w:document ${W}><w:body>${body}<w:sectPr/></w:body></w:document>`);
    const bytes = await pkg.save('application/octet-stream');
    const { document } = await docxToDocument(bytes, { id: 'd', name: 'd.docx' });
    return { document, bytes, first: Object.values(document.tableSource ?? {})[0] };
  }
  const visible = (b: { color: { rgb?: string | null }; width?: { v: number } } | undefined) => !!b && b.color.rgb !== 'transparent' && (b.width?.v ?? 1) > 0;

  it('states every side, invisible where Word draws nothing, so Univer does not add its default line', async () => {
    const { first } = await open(table(''));
    const c = first.tableRows[0].tableCells[0];
    for (const side of [c.borderTop, c.borderBottom, c.borderLeft, c.borderRight]) {
      expect(side).toBeDefined();
      expect(visible(side)).toBe(false);
    }
  });

  it('takes only the sides the table sets', async () => {
    const { first } = await open(table(`<w:tblBorders>${line('bottom')}${line('insideH')}</w:tblBorders>`));
    const [top, bottom] = first.tableRows.map(r => r.tableCells[0]);
    expect(visible(top.borderTop)).toBe(false);
    expect(visible(top.borderBottom)).toBe(true);
    expect(visible(bottom.borderBottom)).toBe(true);
    expect(visible(top.borderLeft)).toBe(false);
    expect(visible(top.borderRight)).toBe(false);
  });

  it('follows the table style and its basedOn, and lets the table and the cell override', async () => {
    const styled = await open(table('<w:tblStyle w:val="Derived"/>'));
    const row = styled.first.tableRows[0].tableCells;
    expect(visible(row[0].borderTop)).toBe(true);
    expect(visible(row[0].borderLeft)).toBe(true);
    expect(visible(row[0].borderRight)).toBe(false); // insideV is off in the derived style

    const off = await open(table(`<w:tblStyle w:val="Base"/><w:tblBorders>${line('top', 'none', 0)}</w:tblBorders>`));
    expect(visible(off.first.tableRows[0].tableCells[0].borderTop)).toBe(false);

    const own = await open(table('<w:tblStyle w:val="Base"/>', 1, [cell(`<w:tcBorders>${line('left', 'nil', 0)}</w:tcBorders>`), cell()]));
    expect(visible(own.first.tableRows[0].tableCells[0].borderLeft)).toBe(false);
  });

  it('applies a first-row border only where tblLook turns it on', async () => {
    const on = await open(table('<w:tblStyle w:val="Header"/><w:tblLook w:firstRow="1"/>'));
    expect(visible(on.first.tableRows[0].tableCells[0].borderBottom)).toBe(true);
    expect(visible(on.first.tableRows[1].tableCells[0].borderBottom)).toBe(false);
    const hex = await open(table('<w:tblStyle w:val="Header"/><w:tblLook w:val="0020"/>'));
    expect(visible(hex.first.tableRows[0].tableCells[0].borderBottom)).toBe(true);
    const off = await open(table('<w:tblStyle w:val="Header"/><w:tblLook w:firstRow="0"/>'));
    expect(visible(off.first.tableRows[0].tableCells[0].borderBottom)).toBe(false);
  });

  it('saves an invisible side as nil, a real one as itself, and reads the same back', async () => {
    const { document, bytes } = await open(table(`<w:tblBorders>${line('bottom')}</w:tblBorders>`, 1));
    const saved = await documentToDocx(document, bytes);
    const xml = (await (await OoxmlPackage.open(saved.bytes)).text('word/document.xml')) ?? '';
    expect(xml).toContain('<w:top w:val="nil"/>');
    expect(xml).not.toContain('transparent');
    expect(xml).toMatch(/<w:bottom w:val="single" w:sz="8"/);
    const again = await docxToDocument(saved.bytes, { id: 'd', name: 'd.docx' });
    const c = Object.values(again.document.tableSource ?? {})[0].tableRows[0].tableCells[0];
    expect(visible(c.borderTop)).toBe(false);
    expect(visible(c.borderBottom)).toBe(true);
  });

  it('writes the line Univer draws for a table made in the editor, so Word shows it too', async () => {
    const { document, first } = await open(table(''));
    const made = first.tableRows[0].tableCells[0];
    delete made.borderTop;
    const xml = (await (await OoxmlPackage.open((await documentToDocx(document)).bytes)).text('word/document.xml')) ?? '';
    expect(xml).toMatch(/<w:top w:val="single" w:sz="4" w:space="0" w:color="C7C9CC"\/>/);
  });
});

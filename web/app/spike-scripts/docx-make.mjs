// Generates sample.docx with the features the ADR §7 fidelity table names, then runs mammoth on it.
import fs from 'node:fs';
import { Document, Packer, Paragraph, TextRun, HeadingLevel, Table, TableRow, TableCell, WidthType, ShadingType, ImageRun, Header, Footer, PageBreak, ExternalHyperlink, AlignmentType, PageNumber, FootnoteReferenceRun } from 'docx';
import mammoth from 'mammoth';

const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAEAAAAAgCAYAAACinX6EAAAAQklEQVR42u3OMQEAAAgDoNm/9Cz4E4kwQ6ZJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZNmjRp0qRJkyZN2uUBZl0AoYzvRCEAAAAASUVORK5CYII=', 'base64');
const doc = new Document({
  footnotes: { 1: { children: [new Paragraph('A footnote body.')] } },
  styles: { paragraphStyles: [{ id: 'Quote', name: 'Quote', basedOn: 'Normal', run: { italics: true, color: '555555' }, paragraph: { indent: { left: 720 } } }] },
  sections: [{
    headers: { default: new Header({ children: [new Paragraph({ text: 'Spike header', alignment: AlignmentType.RIGHT })] }) },
    footers: { default: new Footer({ children: [new Paragraph({ children: [new TextRun('Page '), new TextRun({ children: [PageNumber.CURRENT] })] })] }) },
    children: [
      new Paragraph({ text: 'Fidelity sample', heading: HeadingLevel.HEADING_1 }),
      new Paragraph({ children: [new TextRun('Plain, '), new TextRun({ text: 'bold, ', bold: true }), new TextRun({ text: 'italic, ', italics: true }), new TextRun({ text: 'underlined, ', underline: {} }), new TextRun({ text: 'red 14pt', color: 'CC0000', size: 28 }), new TextRun({ text: ' with a footnote', }), new FootnoteReferenceRun(1)] }),
      new Paragraph({ children: [new ExternalHyperlink({ children: [new TextRun({ text: 'A hyperlink', style: 'Hyperlink' })], link: 'https://example.com' })] }),
      new Paragraph({ text: 'Second level', heading: HeadingLevel.HEADING_2 }),
      new Paragraph({ text: 'Bullet one', bullet: { level: 0 } }),
      new Paragraph({ text: 'Bullet two', bullet: { level: 0 } }),
      new Paragraph({ text: 'Nested bullet', bullet: { level: 1 } }),
      new Paragraph({ text: 'A quoted paragraph using a custom style.', style: 'Quote' }),
      new Table({ width: { size: 100, type: WidthType.PERCENTAGE }, rows: [
        new TableRow({ tableHeader: true, children: ['Name', 'Qty', 'Price'].map(t => new TableCell({ shading: { type: ShadingType.CLEAR, fill: 'D9E2F3' }, children: [new Paragraph({ children: [new TextRun({ text: t, bold: true })] })] })) }),
        new TableRow({ children: ['Widget', '3', '$4.50'].map(t => new TableCell({ children: [new Paragraph(t)] })) }),
        new TableRow({ children: [new TableCell({ columnSpan: 2, children: [new Paragraph('Merged cell')] }), new TableCell({ children: [new Paragraph('$13.50')] })] }),
      ] }),
      new Paragraph({ children: [new ImageRun({ type: 'png', data: png, transformation: { width: 128, height: 64 } })] }),
      new Paragraph({ children: [new PageBreak()] }),
      new Paragraph({ text: 'After the page break', heading: HeadingLevel.HEADING_2 }),
      new Paragraph({ text: 'Centered text.', alignment: AlignmentType.CENTER }),
    ],
  }],
});
const buf = await Packer.toBuffer(doc);
fs.writeFileSync(new URL('./sample.docx', import.meta.url), buf);
console.log('sample.docx bytes', buf.length);
const r = await mammoth.convertToHtml({ buffer: buf });
fs.writeFileSync(new URL('./sample.mammoth.html', import.meta.url), r.value);
console.log('mammoth html chars', r.value.length);
console.log('mammoth messages:', r.messages.map(m => `${m.type}: ${m.message}`));
console.log('mammoth kept: heading', /<h1>/.test(r.value), 'bold', /<strong>/.test(r.value), 'italic', /<em>/.test(r.value), 'table', /<table>/.test(r.value), 'image', /<img/.test(r.value), 'list', /<ul>|<ol>/.test(r.value), 'link', /<a href/.test(r.value), 'footnote', /footnote/i.test(r.value), 'header text', /Spike header/.test(r.value), 'color', /CC0000|color/i.test(r.value), 'underline', /<u>/.test(r.value), 'merged cell', /colspan/.test(r.value), 'centered', /center/.test(r.value));

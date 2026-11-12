import { OoxmlPackage } from '../shared/ooxml';

/**
 * A .docx as Word writes threaded comments (ADR 0x045): "three features" carries a thread of two
 * (Dana Reyes, then Sam Okafor's reply mentioning her) and "fixed" a resolved one, with durable
 * ids 5D3E2F01-03 in commentsIds and people in people.xml.
 */
const W = 'http://schemas.openxmlformats.org/wordprocessingml/2006/main';
const W14 = 'http://schemas.microsoft.com/office/word/2010/wordml';
const W15 = 'http://schemas.microsoft.com/office/word/2012/wordml';
const CID = 'http://schemas.microsoft.com/office/word/2016/wordml/cid';
const CEX = 'http://schemas.microsoft.com/office/word/2018/wordml/cex';
const HEAD = '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>';
const rel = (id: string, type: string, target: string) => `<Relationship Id="${id}" Type="${type}" Target="${target}"/>`;
const OD = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships';
const MS = 'http://schemas.microsoft.com/office';

const DOCUMENT_XML =
  `${HEAD}<w:document xmlns:w="${W}"><w:body>` +
  `<w:p><w:r><w:t xml:space="preserve">We will ship </w:t></w:r><w:commentRangeStart w:id="0"/><w:commentRangeStart w:id="1"/><w:r><w:t>three features</w:t></w:r>` +
  `<w:commentRangeEnd w:id="0"/><w:r><w:commentReference w:id="0"/></w:r><w:commentRangeEnd w:id="1"/><w:r><w:commentReference w:id="1"/></w:r><w:r><w:t xml:space="preserve"> this quarter.</w:t></w:r></w:p>` +
  `<w:p><w:r><w:t xml:space="preserve">Budget is </w:t></w:r><w:commentRangeStart w:id="2"/><w:r><w:t>fixed</w:t></w:r><w:commentRangeEnd w:id="2"/><w:r><w:commentReference w:id="2"/></w:r><w:r><w:t>.</w:t></w:r></w:p>` +
  `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/></w:sectPr></w:body></w:document>`;

const comment = (id: number, author: string, date: string, paraId: string, text: string) =>
  `<w:comment w:id="${id}" w:author="${author}" w:date="${date}" w:initials="X"><w:p w14:paraId="${paraId}" w14:textId="77777777"><w:r><w:annotationRef/></w:r><w:r><w:t>${text}</w:t></w:r></w:p></w:comment>`;

export const COMMENTS_XML =
  `${HEAD}<w:comments xmlns:w="${W}" xmlns:w14="${W14}">` +
  comment(0, 'Dana Reyes', '2026-11-02T09:15:00Z', '1A2B3C01', 'Is three realistic?') +
  comment(1, 'Sam Okafor', '2026-11-02T10:02:00Z', '1A2B3C02', 'Yes. @Dana Reyes can you confirm?') +
  comment(2, 'Dana Reyes', '2026-11-03T14:40:00Z', '1A2B3C03', 'Finance signed this off.') +
  `</w:comments>`;
export const EXTENDED_XML =
  `${HEAD}<w15:commentsEx xmlns:w15="${W15}"><w15:commentEx w15:paraId="1A2B3C01" w15:done="0"/>` +
  `<w15:commentEx w15:paraId="1A2B3C02" w15:paraIdParent="1A2B3C01" w15:done="0"/><w15:commentEx w15:paraId="1A2B3C03" w15:done="1"/></w15:commentsEx>`;
const IDS_XML =
  `${HEAD}<w16cid:commentsIds xmlns:w16cid="${CID}"><w16cid:commentId w16cid:paraId="1A2B3C01" w16cid:durableId="5D3E2F01"/>` +
  `<w16cid:commentId w16cid:paraId="1A2B3C02" w16cid:durableId="5D3E2F02"/><w16cid:commentId w16cid:paraId="1A2B3C03" w16cid:durableId="5D3E2F03"/></w16cid:commentsIds>`;
const EXTENSIBLE_XML = `${HEAD}<w16cex:commentsExtensible xmlns:w16cex="${CEX}"><w16cex:commentExtensible w16cex:durableId="5D3E2F01" w16cex:dateUtc="2026-11-02T17:15:00Z"/></w16cex:commentsExtensible>`;
export const PEOPLE_XML =
  `${HEAD}<w15:people xmlns:w15="${W15}"><w15:person w15:author="Dana Reyes"><w15:presenceInfo w15:providerId="None" w15:userId="Dana Reyes"/></w15:person>` +
  `<w15:person w15:author="Sam Okafor"><w15:presenceInfo w15:providerId="None" w15:userId="Sam Okafor"/></w15:person></w15:people>`;

export async function commentedDocx(): Promise<Uint8Array> {
  const pkg = OoxmlPackage.empty();
  pkg.put(
    '[Content_Types].xml',
    `${HEAD}<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
      `<Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
  );
  pkg.put(
    '_rels/.rels',
    `${HEAD}<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${rel('rId1', `${OD}/officeDocument`, 'word/document.xml')}</Relationships>`,
  );
  pkg.put(
    'word/_rels/document.xml.rels',
    `${HEAD}<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
      rel('rId1', `${OD}/comments`, 'comments.xml') +
      rel('rId2', `${MS}/2011/relationships/commentsExtended`, 'commentsExtended.xml') +
      rel('rId3', `${MS}/2016/09/relationships/commentsIds`, 'commentsIds.xml') +
      rel('rId4', `${MS}/2018/08/relationships/commentsExtensible`, 'commentsExtensible.xml') +
      rel('rId5', `${MS}/2011/relationships/people`, 'people.xml') +
      `</Relationships>`,
  );
  pkg.put('word/document.xml', DOCUMENT_XML);
  pkg.put('word/comments.xml', COMMENTS_XML);
  pkg.put('word/commentsExtended.xml', EXTENDED_XML);
  pkg.put('word/commentsIds.xml', IDS_XML);
  pkg.put('word/commentsExtensible.xml', EXTENSIBLE_XML);
  pkg.put('word/people.xml', PEOPLE_XML);
  return pkg.save('application/vnd.openxmlformats-officedocument.wordprocessingml.document');
}

import { FidelityCollector } from '../shared/fidelity';
import { NS, OoxmlPackage, Relationship, attr, children, relsPathFor, resolveTarget } from '../shared/ooxml';
import {
  RESOURCE_CONDITIONAL_FORMATTING,
  RESOURCE_DATA_VALIDATION,
  RESOURCE_NOTES,
  UniverCfRule,
  UniverDataValidationRule,
  UniverNote,
  UniverWorkbook,
  readResource,
  writeResource,
} from './univer-sheet-types';
import { CfExtras, DxfTable, conditionalFormattingXml, parseConditionalFormatting } from './xlsx-cf';
import { dataValidationXml, parseDataValidation } from './xlsx-dv';
import { ExcelThemeColors } from './xlsx-theme';
import {
  CT_PERSON,
  CT_THREADED_COMMENT,
  REL_PERSON,
  REL_THREADED_COMMENT,
  RESOURCE_THREAD_COMMENTS,
  ThreadExtras,
  ThreadIds,
  ThreadedWriteState,
  UniverComment,
  linkLegacyComments,
  parsePersons,
  parseThreadedComments,
  personsXml,
  threadedCommentsXml,
} from './xlsx-threaded-comments';

/**
 * Conditional formatting, data validation and threaded comments, per sheet, between the package and
 * Univer's resources. All three are handled outside ExcelJS (ADR 0x043), so this runs on the package
 * after ExcelJS has written it, and after the preserved parts have gone back in.
 */

export interface SheetRuleExtras {
  cf: Record<string, CfExtras>;
  /** Validation rules Univer has no model for, by sheet id. */
  rawValidation: Record<string, string[]>;
  threads: ThreadExtras;
}

const REL_COMMENTS = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/comments';
const WB_PATH = 'xl/workbook.xml';
const PERSON_PATH = 'xl/persons/person.xml';
const STYLES_PATH = 'xl/styles.xml';
const MAIN_NS = 'http://schemas.openxmlformats.org/spreadsheetml/2006/main';
/** What follows <conditionalFormatting> and <dataValidations> in CT_Worksheet. */
const AFTER_RULES = [
  'hyperlinks',
  'printOptions',
  'pageMargins',
  'pageSetup',
  'headerFooter',
  'rowBreaks',
  'colBreaks',
  'customProperties',
  'cellWatches',
  'ignoredErrors',
  'smartTags',
  'drawing',
  'legacyDrawing',
  'legacyDrawingHF',
  'picture',
  'oleObjects',
  'controls',
  'webPublishItems',
  'tableParts',
  'extLst',
];

async function sheetPaths(pkg: OoxmlPackage): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  const wb = await pkg.xml(WB_PATH);
  if (!wb) return out;
  const rels = await pkg.rels(WB_PATH);
  const sheets = children(wb.documentElement, MAIN_NS, 'sheets')[0];
  for (const sheet of children(sheets, MAIN_NS, 'sheet')) {
    const relId = attr(sheet, NS.r, 'id');
    const rel = relId ? rels.get(relId) : undefined;
    if (rel) out.set(sheet.getAttribute('name') ?? '', resolveTarget(WB_PATH, rel.target));
  }
  return out;
}

/** Reads the rules and threads of every sheet into the workbook's resources; returns what rides beside them. */
export async function readSheetRules(pkg: OoxmlPackage, workbook: UniverWorkbook, theme: ExcelThemeColors, fidelity: FidelityCollector): Promise<SheetRuleExtras> {
  const extras: SheetRuleExtras = { cf: {}, rawValidation: {}, threads: { persons: [], mentions: {} } };
  const paths = await sheetPaths(pkg);
  const styles = await pkg.xml(STYLES_PATH);
  const dxfs = children(children(styles?.documentElement, MAIN_NS, 'dxfs')[0], MAIN_NS, 'dxf');
  const wbRels = await pkg.rels(WB_PATH);
  const personRel = [...wbRels.values()].find(r => r.type === REL_PERSON);
  extras.threads.persons = parsePersons(personRel ? await pkg.xml(resolveTarget(WB_PATH, personRel.target)) : null);

  const validation: Record<string, UniverDataValidationRule[]> = {};
  const formatting: Record<string, UniverCfRule[]> = {};
  const threads: Record<string, UniverComment[]> = {};
  const notes = readResource<Record<string, Record<number, Record<number, UniverNote>>>>(workbook, RESOURCE_NOTES) ?? {};
  let notesChanged = false;
  for (const sheetId of workbook.sheetOrder) {
    const name = workbook.sheets[sheetId].name;
    const path = paths.get(name);
    const doc = path ? await pkg.xml(path) : null;
    if (!path || !doc) continue;
    const cf = parseConditionalFormatting(doc.documentElement, dxfs, theme, fidelity, name, `cf-${sheetId}`);
    if (cf.rules.length) formatting[sheetId] = cf.rules;
    Object.assign(extras.cf, cf.extras);
    const dv = parseDataValidation(doc.documentElement, `dv-${sheetId}`, fidelity, name);
    if (dv.rules.length) validation[sheetId] = dv.rules;
    if (dv.rawRegular.length) extras.rawValidation[sheetId] = dv.rawRegular;
    const threadRel = [...(await pkg.rels(path)).values()].find((r: Relationship) => r.type === REL_THREADED_COMMENT);
    if (threadRel) {
      const parsed = parseThreadedComments(await pkg.xml(resolveTarget(path, threadRel.target)), extras.threads.persons, workbook.id, sheetId, extras.threads.mentions);
      if (parsed.length) threads[sheetId] = parsed;
      // A thread's legacy note is its placeholder for old versions of Excel, not a note of its own.
      for (const thread of parsed) {
        const m = /^([A-Z]+)(\d+)$/.exec(thread.ref);
        if (!m) continue;
        const row = Number(m[2]) - 1;
        let col = 0;
        for (const ch of m[1]) col = col * 26 + ch.charCodeAt(0) - 64;
        if (notes[sheetId]?.[row]?.[col - 1]) {
          delete notes[sheetId][row][col - 1];
          notesChanged = true;
        }
      }
    }
  }
  if (Object.keys(validation).length) writeResource(workbook, RESOURCE_DATA_VALIDATION, validation);
  if (Object.keys(formatting).length) writeResource(workbook, RESOURCE_CONDITIONAL_FORMATTING, formatting);
  if (Object.keys(threads).length) writeResource(workbook, RESOURCE_THREAD_COMMENTS, threads);
  if (notesChanged) writeResource(workbook, RESOURCE_NOTES, notes);
  return extras;
}

function insertRules(xml: string, fragment: string): string {
  let at = -1;
  for (const name of AFTER_RULES) {
    const i = xml.search(new RegExp(`<${name}[\\s>/]`));
    if (i !== -1 && (at === -1 || i < at)) at = i;
  }
  if (at === -1) at = xml.lastIndexOf('</worksheet>');
  return xml.slice(0, at) + fragment + xml.slice(at);
}

function addExtensions(xml: string, ext: string): string {
  if (/<extLst\b[^>]*\/>/.test(xml)) return xml.replace(/<extLst\b[^>]*\/>/, `<extLst>${ext}</extLst>`);
  // The sheet's own extLst is its last one; a rule's has a <cfRule> around it.
  const close = xml.lastIndexOf('</extLst>');
  const end = xml.lastIndexOf('</worksheet>');
  if (close !== -1 && xml.slice(close + 9, end).trim() === '') return xml.slice(0, close) + ext + xml.slice(close);
  return xml.slice(0, end) + `<extLst>${ext}</extLst>` + xml.slice(end);
}

function writeDxfs(xml: string, table: DxfTable, base: number): string {
  if (!table.elements.length) return xml;
  const added = table.elements.map(e => `<dxf>${e}</dxf>`).join('');
  const count = base + table.elements.length;
  if (/<dxfs\b[^>]*\/>/.test(xml)) return xml.replace(/<dxfs\b[^>]*\/>/, `<dxfs count="${count}">${added}</dxfs>`);
  if (/<\/dxfs>/.test(xml)) return xml.replace(/<dxfs\b[^>]*>/, `<dxfs count="${count}">`).replace('</dxfs>', `${added}</dxfs>`);
  return xml.replace(/<tableStyles\b|<\/styleSheet>/, m => `<dxfs count="${count}">${added}</dxfs>${m}`);
}

function addOverride(types: string, part: string, contentType: string): string {
  return types.includes(`PartName="/${part}"`) ? types : types.replace('</Types>', `<Override PartName="/${part}" ContentType="${contentType}"/></Types>`);
}

function relsXml(rels: Relationship[]): string {
  const entries = rels
    .map(r => `<Relationship Id="${r.id}" Type="${r.type}" Target="${r.target.replace(/&/g, '&amp;').replace(/"/g, '&quot;')}"${r.external ? ' TargetMode="External"' : ''}/>`)
    .join('');
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="${NS.rel}">${entries}</Relationships>`;
}

function nextRelId(rels: Relationship[]): string {
  let n = rels.length + 1;
  while (rels.some(r => r.id === `rId${n}`)) n++;
  return `rId${n}`;
}

/** Writes rules and threads into a package that holds the workbook's sheets. */
export async function writeSheetRules(bytes: Uint8Array, workbook: UniverWorkbook, extras: SheetRuleExtras | undefined, fidelity: FidelityCollector, mimeType: string): Promise<Uint8Array> {
  const validation = readResource<Record<string, UniverDataValidationRule[]>>(workbook, RESOURCE_DATA_VALIDATION) ?? {};
  const formatting = readResource<Record<string, UniverCfRule[]>>(workbook, RESOURCE_CONDITIONAL_FORMATTING) ?? {};
  const threads = readResource<Record<string, UniverComment[]>>(workbook, RESOURCE_THREAD_COMMENTS) ?? {};
  const rawValidation = extras?.rawValidation ?? {};
  const hasAny = (m: Record<string, unknown[]>) => Object.values(m).some(list => list?.length);
  if (!hasAny(validation) && !hasAny(formatting) && !hasAny(threads) && !hasAny(rawValidation) && !extras?.threads.persons.length) return bytes;

  const pkg = await OoxmlPackage.open(bytes);
  const paths = await sheetPaths(pkg);
  let stylesXml = (await pkg.text(STYLES_PATH)) ?? '';
  const base = Number(/<dxfs\b[^>]*\bcount="(\d+)"/.exec(stylesXml)?.[1] ?? 0);
  const dxfs = new DxfTable(base);
  let types = (await pkg.text('[Content_Types].xml')) ?? '';
  const state: ThreadedWriteState = {
    ids: new ThreadIds(),
    persons: new Map((extras?.threads.persons ?? []).map(p => [p.id, p])),
    mentions: extras?.threads.mentions ?? {},
  };
  let threadParts = 0;
  for (const sheetId of workbook.sheetOrder) {
    const name = workbook.sheets[sheetId]?.name;
    const path = paths.get(name);
    if (!path) continue;
    const original = (await pkg.text(path)) ?? '';
    let xml = original;
    const cf = conditionalFormattingXml(formatting[sheetId] ?? [], extras?.cf, dxfs, fidelity, name);
    const dv = dataValidationXml(validation[sheetId] ?? [], rawValidation[sheetId] ?? [], fidelity, name);
    if (cf.regular || dv.regular) xml = insertRules(xml, cf.regular + dv.regular);
    if (cf.ext || dv.ext) xml = addExtensions(xml, cf.ext + dv.ext);
    if (xml !== original) pkg.put(path, xml);
    const sheetThreads = threads[sheetId] ?? [];
    const part = threadedCommentsXml(sheetThreads, state);
    if (!part) continue;
    threadParts += 1;
    const partPath = `xl/threadedComments/threadedComment${threadParts}.xml`;
    pkg.put(partPath, part);
    types = addOverride(types, partPath, CT_THREADED_COMMENT);
    const rels = [...(await pkg.rels(path)).values()];
    const comments = rels.find(r => r.type === REL_COMMENTS);
    if (comments) {
      const commentsPath = resolveTarget(path, comments.target);
      const legacy = await pkg.text(commentsPath);
      if (legacy) pkg.put(commentsPath, linkLegacyComments(legacy, sheetThreads, state.ids));
    }
    rels.push({ id: nextRelId(rels), type: REL_THREADED_COMMENT, target: `../threadedComments/threadedComment${threadParts}.xml`, external: false });
    pkg.put(relsPathFor(path), relsXml(rels));
  }
  if (state.persons.size) {
    pkg.put(PERSON_PATH, personsXml(state.persons.values()));
    types = addOverride(types, PERSON_PATH, CT_PERSON);
    const wbRels = [...(await pkg.rels(WB_PATH)).values()];
    wbRels.push({ id: nextRelId(wbRels), type: REL_PERSON, target: 'persons/person.xml', external: false });
    pkg.put(relsPathFor(WB_PATH), relsXml(wbRels));
  }
  if (stylesXml) {
    stylesXml = writeDxfs(stylesXml, dxfs, base);
    pkg.put(STYLES_PATH, stylesXml);
  }
  pkg.put('[Content_Types].xml', types);
  return pkg.save(mimeType);
}


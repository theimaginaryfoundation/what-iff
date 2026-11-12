import { describe, expect, it } from 'vitest';

import { COMMENT_CELL_KEY, SheetCommentThread, commentCells } from './sheet-comments';
import { UniverWorkbook } from './univer-sheet-types';

describe('commentCells', () => {
  const tagged = { custom: { [COMMENT_CELL_KEY]: 't1' } };
  const workbook = {
    id: 'wb',
    name: 'wb',
    appVersion: '',
    locale: 'en',
    sheetOrder: ['s1'],
    sheets: { s1: { id: 's1', name: 'Sheet1', cellData: { 0: { 0: tagged }, 4: { 2: tagged } } } },
    styles: {},
    resources: [],
  } as unknown as UniverWorkbook;
  const thread = (quote: string): SheetCommentThread => ({ id: 't1', anchor: { sheetId: 's1' }, quote, comments: [] });

  it('keeps a thread on the cell it names when a copy of that cell carries its tag too', () => {
    expect(commentCells(workbook, [thread('C5')]).get('t1')).toEqual({ sheetId: 's1', row: 4, col: 2 });
  });

  it('takes the first tagged cell when the thread names none of them', () => {
    expect(commentCells(workbook, [thread('Z9')]).get('t1')).toEqual({ sheetId: 's1', row: 0, col: 0 });
    expect(commentCells(workbook).get('t1')).toEqual({ sheetId: 's1', row: 0, col: 0 });
  });
});

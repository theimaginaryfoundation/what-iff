import { cellA1, columnIndex, columnLetter, parseCellA1, parseRangeA1, parseSqref, rangeA1 } from './a1';

describe('A1 references', () => {
  it('converts columns both ways, including past Z', () => {
    expect(columnLetter(0)).toBe('A');
    expect(columnLetter(25)).toBe('Z');
    expect(columnLetter(26)).toBe('AA');
    expect(columnLetter(701)).toBe('ZZ');
    expect(columnLetter(702)).toBe('AAA');
    for (const i of [0, 25, 26, 51, 701, 702, 16383]) expect(columnIndex(columnLetter(i))).toBe(i);
  });

  it('parses cells and ranges, normalising order and ignoring $ and sheet names', () => {
    expect(cellA1(0, 0)).toBe('A1');
    expect(parseCellA1('$C$3')).toEqual({ row: 2, col: 2 });
    expect(parseCellA1('3C')).toBeNull();
    expect(parseRangeA1('Sheet1!C3:A1')).toEqual({ startRow: 0, startColumn: 0, endRow: 2, endColumn: 2 });
    expect(parseRangeA1('B2')).toEqual({ startRow: 1, startColumn: 1, endRow: 1, endColumn: 1 });
    expect(rangeA1({ startRow: 0, startColumn: 0, endRow: 2, endColumn: 2 })).toBe('A1:C3');
    expect(rangeA1({ startRow: 1, startColumn: 1, endRow: 1, endColumn: 1 })).toBe('B2');
    expect(parseSqref('A1:B2 nope D4')).toEqual([
      { startRow: 0, startColumn: 0, endRow: 1, endColumn: 1 },
      { startRow: 3, startColumn: 3, endRow: 3, endColumn: 3 },
    ]);
  });
});

import { describe, expect, it } from 'vitest';

import { inlineSourceMap, originalLine } from './source-map';

// esbuild's output for list.ts (types removed, so its lines move up by two).
const COMPILED =
  'function add(list, item) {\n  const li = document.createElement("li");\n  li.textContent = item.name;\n  list.append(li);\n}\nexport {\n  add\n};\n//# sourceMappingURL=data:application/json;base64,ewogICJ2ZXJzaW9uIjogMywKICAic291cmNlcyI6IFsibGlzdC50cyJdLAogICJzb3VyY2VzQ29udGVudCI6IFsidHlwZSBJdGVtID0geyBuYW1lOiBzdHJpbmcgfTtcblxuZXhwb3J0IGZ1bmN0aW9uIGFkZChsaXN0OiBIVE1MRWxlbWVudCwgaXRlbTogSXRlbSk6IHZvaWQge1xuICBjb25zdCBsaTogSFRNTExJRWxlbWVudCA9IGRvY3VtZW50LmNyZWF0ZUVsZW1lbnQoJ2xpJyk7XG4gIGxpLnRleHRDb250ZW50ID0gaXRlbS5uYW1lO1xuICBsaXN0LmFwcGVuZChsaSk7XG59XG4iXSwKICAibWFwcGluZ3MiOiAiQUFFTyxTQUFTLElBQUksTUFBbUIsTUFBa0I7QUFDdkQsUUFBTSxLQUFvQixTQUFTLGNBQWMsSUFBSTtBQUNyRCxLQUFHLGNBQWMsS0FBSztBQUN0QixPQUFLLE9BQU8sRUFBRTtBQUNoQjsiLAogICJuYW1lcyI6IFtdCn0K\n';

describe('originalLine', () => {
  it('maps a place in compiled code to the line of the file it came from', () => {
    const map = inlineSourceMap(COMPILED)!;
    expect(map).not.toBeNull();
    // document.createElement('li'), on line 2 of the output, is line 4 of list.ts.
    expect(originalLine(map.mappings, 2, 14)).toBe(4);
    expect(originalLine(map.mappings, 1, 1)).toBe(3);
    expect(originalLine(map.mappings, 4, 3)).toBe(6);
    // Past the mappings, or a line with none.
    expect(originalLine(map.mappings, 40, 1)).toBeNull();
    expect(originalLine(map.mappings, 7, 1)).toBeNull();
  });

  it('finds no map in code without one, or with a broken one', () => {
    expect(inlineSourceMap('export {};')).toBeNull();
    expect(inlineSourceMap('x;\n//# sourceMappingURL=data:application/json;base64,!!!')).toBeNull();
  });
});

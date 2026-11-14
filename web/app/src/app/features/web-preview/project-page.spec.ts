import { describe, expect, it } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';
import { pageAmong, projectPageOf } from './project-page';

const f = (id: string, name: string, folder = ''): FileAttachment => ({ id, name, folder }) as FileAttachment;

/** Gallery folders by name, and one conversation's files by path. */
function folders(gallery: Record<string, FileAttachment[]>, conversation: Record<string, FileAttachment> = {}): FolderFiles {
  return {
    list: async (folder: string) => new Map((gallery[folder] ?? []).map(file => [file.name, file])),
    conversation: async () => new Map(Object.entries(conversation)),
  } as unknown as FolderFiles;
}

describe('pageAmong', () => {
  it('is the folder’s index.html, else its only page', () => {
    const index = f('i', 'Index.HTML');
    expect(
      pageAmong([
        ['a.html', f('a', 'a.html')],
        ['Index.HTML', index],
      ]),
    ).toBe(index);
    const only = f('o', 'demo.htm');
    expect(
      pageAmong([
        ['demo.htm', only],
        ['app.js', f('j', 'app.js')],
      ]),
    ).toBe(only);
    expect(
      pageAmong([
        ['a.html', f('a', 'a.html')],
        ['b.html', f('b', 'b.html')],
      ]),
    ).toBeNull();
    expect(pageAmong([])).toBeNull();
  });
});

describe('projectPageOf', () => {
  const page = f('page', 'index.html', 'site');

  it('finds the page in the file’s folder or the nearest one above it, but not at the gallery’s top level', async () => {
    const gallery = { site: [page], 'site/src': [f('s', 'app.js', 'site/src')], '': [f('top', 'index.html')] };
    expect(await projectPageOf(f('s', 'app.js', 'site/src'), folders(gallery), null)).toBe(page);
    expect(await projectPageOf(f('n', 'notes.md', 'notes'), folders(gallery), null)).toBeNull();
    expect(await projectPageOf(f('t', 'app.js'), folders(gallery), null)).toBeNull();
    // A page has no page of its own to open.
    expect(await projectPageOf(page, folders(gallery), null)).toBeNull();
  });

  it('finds a folderless file’s page among the conversation’s files, by its path there', async () => {
    const top = f('top', 'index.html');
    const copy = f('copy', 'app.js');
    const conversation = { 'index.html': top, 'site/index.html': page, 'site/js/app.js': copy, 'main.js': f('m', 'main.js') };
    expect(await projectPageOf(copy, folders({}, conversation), 'c-1')).toBe(page);
    expect(await projectPageOf(f('m', 'main.js'), folders({}, conversation), 'c-1')).toBe(top);
    expect(await projectPageOf(f('x', 'other.js'), folders({}, conversation), 'c-1')).toBeNull();
  });
});

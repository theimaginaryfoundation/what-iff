import { FileAttachment } from '../../core/models/file-attachment.model';
import { FolderFiles } from './folder-files.service';

/** Files a page is: what a project's page is found among. */
const PAGE = /\.html?$/i;

/** The page among a folder's files (by name): its `index.html`, else its only HTML file; null for none. */
export function pageAmong(names: Iterable<[string, FileAttachment]>): FileAttachment | null {
  const pages = [...names].filter(([name]) => PAGE.test(name));
  const index = pages.find(([name]) => /^index\.html?$/i.test(name));
  if (index) return index[1];
  return pages.length === 1 ? pages[0]![1] : null;
}

/** The folder a folder is in ("" for the top level). */
function parentOf(folder: string): string {
  const slash = folder.lastIndexOf('/');
  return slash < 0 ? '' : folder.slice(0, slash);
}

/**
 * The page of the project a file is part of (ADR 0x050 §2): the page in its folder, else in the
 * nearest folder above it that has one, so a script or stylesheet can open the page it builds.
 * The gallery's top level is no project (a page there is not given the whole gallery); a file with
 * no folder, in a conversation, is looked for among the conversation's files by its path there.
 * Null for an HTML file itself, or a file with no page above it.
 */
export async function projectPageOf(file: FileAttachment, folders: FolderFiles, chatId: string | null): Promise<FileAttachment | null> {
  if (PAGE.test(file.name)) return null;
  for (let dir = file.folder ?? ''; dir; dir = parentOf(dir)) {
    const files = await folders.list(dir).catch(() => null);
    const page = files && pageAmong(files);
    if (page) return page;
  }
  if (file.folder || !chatId) return null;
  // A file with no folder of its own (at the top level, or a chat's copy of one): where the
  // conversation has it, and the folders above that.
  const byPath = await folders.conversation(chatId).catch(() => null);
  const path = byPath && [...byPath].find(([, f]) => f.id === file.id)?.[0];
  if (!byPath || !path) return null;
  for (let dir = parentOf(path); ; dir = parentOf(dir)) {
    const prefix = dir ? `${dir}/` : '';
    const here = [...byPath]
      .filter(([p]) => p.startsWith(prefix) && !p.slice(prefix.length).includes('/'))
      .map(([p, f]): [string, FileAttachment] => [p.slice(prefix.length), f]);
    const page = pageAmong(here);
    if (page || !dir) return page;
  }
}

import { SharedFile } from './share-api.service';

/** A folder of a shared web app's file tree: its folders, then its files, each by name. */
export interface TreeFolder {
  name: string;
  /** The folder's path in the project ('' for the root). */
  path: string;
  folders: TreeFolder[];
  files: SharedFile[];
}

/** A row of the tree as it is shown: a folder (open or not) or a file, at a depth. */
export type TreeRow =
  | { kind: 'folder'; path: string; name: string; depth: number; open: boolean }
  | { kind: 'file'; path: string; name: string; depth: number; file: SharedFile };

/** The tree of a web app's files, folders and files each sorted by name (without case). */
export function buildTree(files: readonly SharedFile[]): TreeFolder {
  const root: TreeFolder = { name: '', path: '', folders: [], files: [] };
  for (const file of files) {
    const segments = file.path.split('/');
    let folder = root;
    for (const name of segments.slice(0, -1)) {
      const path = folder.path ? `${folder.path}/${name}` : name;
      let next = folder.folders.find(f => f.path === path);
      if (!next) {
        next = { name, path, folders: [], files: [] };
        folder.folders.push(next);
      }
      folder = next;
    }
    folder.files.push(file);
  }
  const byName = (a: { name?: string; path: string }, b: { name?: string; path: string }) =>
    (a.name ?? a.path).localeCompare(b.name ?? b.path, undefined, { sensitivity: 'base', numeric: true });
  const sort = (folder: TreeFolder) => {
    folder.folders.sort(byName);
    folder.files.sort((a, b) => byName({ path: baseName(a.path) }, { path: baseName(b.path) }));
    folder.folders.forEach(sort);
  };
  sort(root);
  return root;
}

/** The rows shown for a tree, with the folders in `open` opened. */
export function treeRows(root: TreeFolder, open: ReadonlySet<string>): TreeRow[] {
  const rows: TreeRow[] = [];
  const walk = (folder: TreeFolder, depth: number) => {
    for (const sub of folder.folders) {
      const isOpen = open.has(sub.path);
      rows.push({ kind: 'folder', path: sub.path, name: sub.name, depth, open: isOpen });
      if (isOpen) walk(sub, depth + 1);
    }
    for (const file of folder.files) rows.push({ kind: 'file', path: file.path, name: baseName(file.path), depth, file });
  };
  walk(root, 0);
  return rows;
}

/** The folders a path is inside, outermost first ('a/b/c.js' is in 'a' and 'a/b'). */
export function foldersOf(path: string): string[] {
  const segments = path.split('/').slice(0, -1);
  return segments.map((_, i) => segments.slice(0, i + 1).join('/'));
}

export function baseName(path: string): string {
  return path.slice(path.lastIndexOf('/') + 1);
}

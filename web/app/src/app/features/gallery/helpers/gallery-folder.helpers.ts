/**
 * Gallery folders are a path label on each image ("charts/oura"), not a place in storage. These
 * helpers mirror the server's rules (internal/models/folder.go) so the UI never offers a path the
 * API would refuse, and turn the flat list of folders into what one screen shows.
 */

/** A folder that holds images, with how many sit directly in it (GET /image-gallery/folders). */
export interface GalleryFolder {
  path: string;
  count: number;
}

/** A folder tile: one level beneath the folder being viewed. */
export interface FolderTileVm {
  path: string;
  name: string;
  /** Images in this folder and every folder beneath it. */
  count: number;
}

export interface Breadcrumb {
  label: string;
  /** "" is the top level. */
  path: string;
}

export const MAX_FOLDER_DEPTH = 8;
export const MAX_FOLDER_SEGMENT_LENGTH = 64;
export const MAX_FOLDER_PATH_LENGTH = 255;

const CONTROL_CHARACTERS = /[\u0000-\u001f\u007f-\u009f]/;

/** Why a folder path would be refused, or null when it is fine (the top level, "", is fine). */
export function folderPathError(raw: string): string | null {
  const segments = splitFolder(raw);
  for (const segment of segments) {
    if (segment === '.' || segment === '..') {
      return `"${segment}" is not allowed in a folder name`;
    }
    if (CONTROL_CHARACTERS.test(segment)) {
      return 'Folder names cannot contain control characters';
    }
    if ([...segment].length > MAX_FOLDER_SEGMENT_LENGTH) {
      return `A folder name is at most ${MAX_FOLDER_SEGMENT_LENGTH} characters`;
    }
  }
  if (segments.length > MAX_FOLDER_DEPTH) {
    return `Folders nest at most ${MAX_FOLDER_DEPTH} deep`;
  }
  if ([...segments.join('/')].length > MAX_FOLDER_PATH_LENGTH) {
    return `A folder path is at most ${MAX_FOLDER_PATH_LENGTH} characters`;
  }
  return null;
}

/**
 * The canonical form of a folder path: segments trimmed and lower-cased, joined by "/", empty
 * segments dropped ("Charts//Oura/" is "charts/oura"). Returns null for a path the server would
 * refuse; "" is the top level.
 */
export function normalizeFolderPath(raw: string): string | null {
  if (folderPathError(raw) !== null) {
    return null;
  }
  return splitFolder(raw).join('/');
}

function splitFolder(raw: string): string[] {
  return raw
    .split(/[\\/]/)
    .map(segment => segment.trim().toLowerCase())
    .filter(segment => segment !== '');
}

/** True when path is folder itself or beneath it. The top level ("") contains everything. */
export function isWithinFolder(path: string, folder: string): boolean {
  return folder === '' || path === folder || path.startsWith(`${folder}/`);
}

export function folderName(path: string): string {
  const index = path.lastIndexOf('/');
  return index === -1 ? path : path.slice(index + 1);
}

export function parentFolder(path: string): string {
  const index = path.lastIndexOf('/');
  return index === -1 ? '' : path.slice(0, index);
}

/** The path to a folder as clickable steps, starting at the top level. */
export function breadcrumbsFor(path: string): Breadcrumb[] {
  const crumbs: Breadcrumb[] = [{ label: 'Gallery', path: '' }];
  let walked = '';
  for (const segment of path === '' ? [] : path.split('/')) {
    walked = walked === '' ? segment : `${walked}/${segment}`;
    crumbs.push({ label: segment, path: walked });
  }
  return crumbs;
}

/**
 * The folders to show as tiles when viewing `current`: its direct children, each counting every
 * image beneath it. `pending` holds folders created in this session that are still empty (a folder
 * only exists on the server once it holds an image); they show with a count of 0.
 */
export function childFolderTiles(folders: readonly GalleryFolder[], current: string, pending: readonly string[] = []): FolderTileVm[] {
  const counts = new Map<string, number>();
  const note = (path: string, count: number) => {
    if (path === current || !isWithinFolder(path, current)) {
      return;
    }
    const rest = current === '' ? path : path.slice(current.length + 1);
    const first = rest.split('/')[0];
    const child = current === '' ? first : `${current}/${first}`;
    counts.set(child, (counts.get(child) ?? 0) + count);
  };
  for (const folder of folders) {
    note(folder.path, folder.count);
  }
  for (const path of pending) {
    note(path, 0);
  }
  return [...counts.entries()]
    .map(([path, count]) => ({ path, name: folderName(path), count }))
    .sort((a, b) => a.name.localeCompare(b.name));
}

/**
 * Every folder a move could target: each folder that holds images or was created this session,
 * plus all their parents, sorted by path.
 */
export function moveDestinations(folders: readonly GalleryFolder[], pending: readonly string[] = []): string[] {
  const all = new Set<string>();
  for (const path of [...folders.map(folder => folder.path), ...pending]) {
    let walked = '';
    for (const segment of path.split('/')) {
      walked = walked === '' ? segment : `${walked}/${segment}`;
      all.add(walked);
    }
  }
  return [...all].sort((a, b) => a.localeCompare(b));
}

/** What a folder path reads as to a person: the top level is "Gallery". */
export function folderLabel(path: string): string {
  return path === '' ? 'Gallery' : path.split('/').join(' / ');
}

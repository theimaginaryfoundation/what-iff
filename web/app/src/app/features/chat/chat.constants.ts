
// Scroll behavior constants
export const SCROLL_BUTTON_THRESHOLD_PX = 20;
/** When within this many px of the top of the thread, fetch the next older page of messages. */
export const SCROLL_LOAD_OLDER_THRESHOLD_PX = 120;
/** Must match the limit used for initial chat load and message list refreshes. */
export const MESSAGE_LIST_PAGE_SIZE = 50;

// Textarea configuration
export const TEXTAREA_MAX_ROWS = 10;
export const TEXTAREA_LINE_HEIGHT_PX = 24;

// Streaming animation constants
export const STREAMING_INTERVAL_MS = 25;
export const STREAMING_SCROLL_CHECK_INTERVAL = 10;
export const CODE_BLOCK_CHUNK_SIZE = 30;

// Draft autosave configuration
export const AUTOSAVE_DEBOUNCE_MS = 500;

// File upload validation
export const MAX_FILE_SIZE_BYTES = 10 * 1024 * 1024; // 10MB

/** Client-only synthetic assistant row while waiting on a chat job (never returned by the API). */
export const CHAT_PENDING_ASSISTANT_MESSAGE_ID = '00000000-0000-4000-8000-000000000001';

export const ALLOWED_FILE_TYPES = [
  // Images
  'image/png',
  'image/jpeg',
  'image/jpg',
  'image/gif',
  'image/webp',
  // Documents
  'application/pdf',
  'text/plain',
  'text/csv',
  'text/markdown',
  'application/msword',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
  'application/vnd.ms-excel',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
] as const;

export type AllowedFileType = typeof ALLOWED_FILE_TYPES[number];

/**
 * Extensions accepted by the API (`internal/utils/fileutils.go` fileTypes map).
 * Browsers often omit or misreport `File.type`; we fall back to the filename suffix.
 */
export const ALLOWED_UPLOAD_EXTENSIONS = [
  '.c',
  '.cpp',
  '.cs',
  '.css',
  '.csv',
  '.doc',
  '.docx',
  '.gif',
  '.go',
  '.html',
  '.java',
  '.jpeg',
  '.jpg',
  '.js',
  '.json',
  '.md',
  '.pdf',
  '.php',
  '.png',
  '.pptx',
  '.py',
  '.rb',
  '.sh',
  '.tar',
  '.tex',
  '.tgz',
  '.ts',
  '.txt',
  '.xlsx',
  '.xml',
  '.zip'
] as const;

const ALLOWED_UPLOAD_EXT_SET = new Set<string>(ALLOWED_UPLOAD_EXTENSIONS);

/** Last path segment, then suffix from last '.' (handles unicode in basename). */
export function getUploadFilenameExtension(fileName: string): string {
  const base = fileName.split(/[/\\]/).pop() ?? fileName;
  const i = base.lastIndexOf('.');
  if (i <= 0 || i === base.length - 1) {
    return '';
  }
  return base.slice(i).toLowerCase();
}

export function isExtensionAllowedForChatUpload(fileName: string): boolean {
  const ext = getUploadFilenameExtension(fileName);
  return ext !== '' && ALLOWED_UPLOAD_EXT_SET.has(ext);
}

/** True if browser MIME is allowlisted or filename extension is API-supported. */
export function isMimeOrExtensionAllowedForChatUpload(file: File): boolean {
  const mime = (file.type || '').trim();
  if (mime && (ALLOWED_FILE_TYPES as readonly string[]).includes(mime)) {
    return true;
  }
  return isExtensionAllowedForChatUpload(file.name);
}


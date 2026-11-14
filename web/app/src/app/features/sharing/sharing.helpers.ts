import { FileAttachment } from '../../core/models/file-attachment.model';
import { previewKindFor } from '../gallery/helpers/file-preview.helpers';

/** Whether a file is shared as a web app: a web page in a gallery folder (ADR 0x051 §1; models.SharesAsWebApp). */
export function isWebAppShare(file: Pick<FileAttachment, 'name' | 'folder' | 'file_type'>): boolean {
  return !!file.folder && previewKindFor(file) === 'html';
}

/** The kinds the share site shows (ADR 0x051 §1): everything the workspace views, but diagrams. */
const SHAREABLE = new Set(['text', 'markdown', 'mermaid', 'html', 'csv', 'spreadsheet', 'document', 'slides', 'notebook', 'image', 'pdf']);

/** Whether a file can be shared: its kind has a viewer on the share site, and it is no skill's file. */
export function isShareable(file: FileAttachment): boolean {
  return SHAREABLE.has(previewKindFor(file)) && file.origin !== 'skill' && !file.skill_id;
}

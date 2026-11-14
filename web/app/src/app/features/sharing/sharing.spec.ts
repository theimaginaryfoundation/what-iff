import { describe, expect, it } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { shareSiteUrl } from '../../core/services/share-link.service';
import { isShareable, isWebAppShare } from './sharing.helpers';

const file = (over: Partial<FileAttachment>): FileAttachment =>
  ({ id: 'f', name: 'a.txt', file_type: 'text/plain', ...over }) as FileAttachment;

describe('sharing helpers', () => {
  it('shares an HTML page in a folder as a web app, and one at the top level as a file', () => {
    expect(isWebAppShare({ name: 'index.html', folder: 'site', file_type: 'text/html' })).toBe(true);
    expect(isWebAppShare({ name: 'page', folder: 'site/a', file_type: 'text/html' })).toBe(true);
    expect(isWebAppShare({ name: 'index.html', folder: '', file_type: 'text/html' })).toBe(false);
    expect(isWebAppShare({ name: 'app.js', folder: 'site', file_type: 'text/javascript' })).toBe(false);
  });

  it('shares what the share site shows, but no skill’s files and no diagrams', () => {
    expect(isShareable(file({ name: 'notes.md', file_type: 'text/markdown' }))).toBe(true);
    expect(isShareable(file({ name: 'photo.png', file_type: 'image/png' }))).toBe(true);
    expect(isShareable(file({ name: 'SKILL.md', file_type: 'text/markdown', origin: 'skill' }))).toBe(false);
    expect(isShareable(file({ name: 'flow.drawio', file_type: 'application/vnd.jgraph.mxfile' }))).toBe(false);
  });

  it('offers sharing only where the build names a share site', () => {
    expect(shareSiteUrl('https://share.example.com/')).toBe('https://share.example.com');
    expect(shareSiteUrl('__SHARE_URL__')).toBe('');
    expect(shareSiteUrl(undefined)).toBe('');
  });
});

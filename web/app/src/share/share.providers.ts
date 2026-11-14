import { EnvironmentProviders, Provider, signal } from '@angular/core';
import { Observable, of, throwError } from 'rxjs';

import { FileAttachmentService } from '../app/core/services/file-attachment.service';
import { FolderFiles } from '../app/features/web-preview/folder-files.service';
import { CommentPeopleService } from '../app/features/office/comments/comment-people.service';
import { TextCommentsService } from '../app/features/file-workspace/text-comments/text-comments.service';
import { JUPYTERLITE_URL } from '../app/features/notebook/notebook-run.config';
import { PREVIEW_URL } from '../app/features/web-preview/preview-site.config';
import { userPersonId } from '../app/features/office/comments/comment-threads';
import { ShareFolderFiles } from './share-folder-files';
import { shareEnvironment } from './share-environment';

/**
 * The app's viewers, read-only and without an account (ADR 0x051 §6). They reach for the signed-in
 * user's services (comments, page pictures, the gallery); on the share site those are stand-ins
 * that hold nothing and send nothing, so a viewer shows a file and does nothing else.
 */
export function readOnlyViewerProviders(): (Provider | EnvironmentProviders)[] {
  return [
    // Comments are the owner's: none are read, none written.
    {
      provide: TextCommentsService,
      useValue: {
        get: (): Observable<never> => throwError(() => new Error('Comments are not shared')),
        put: (): Observable<never> => throwError(() => new Error('Comments are not shared')),
      },
    },
    { provide: CommentPeopleService, useValue: { me: signal({ id: userPersonId('visitor'), name: 'Visitor' }) } },
    // A page's picture is the owner's to draw: a visitor's view is never stored.
    {
      provide: FileAttachmentService,
      useValue: { hasPreview: () => of(true), putPreview: () => of(undefined) },
    },
    // The share's files stand in for the gallery's.
    { provide: FolderFiles, useExisting: ShareFolderFiles },
    // Notebooks show their saved outputs; nothing runs.
    { provide: JUPYTERLITE_URL, useValue: null },
    { provide: PREVIEW_URL, useFactory: () => previewUrlFor(shareEnvironment.previewUrl, location.origin) },
  ];
}

/**
 * The web apps site's address, when shared web apps can run there: another origin than the share
 * site's (ADR 0x051 §5), on https or this machine. Unlike the app, the share site may sit on the
 * same site as it: it holds nothing a page could take (its unlocks are HttpOnly cookies).
 */
export function previewUrlFor(raw: string | undefined, origin: string): string | null {
  let url: URL;
  try {
    url = new URL((raw ?? '').trim());
  } catch {
    return null;
  }
  const local = url.hostname === 'localhost' || url.hostname === '127.0.0.1';
  if (url.origin === origin || (url.protocol !== 'https:' && !(local && url.protocol === 'http:'))) return null;
  return url.href;
}

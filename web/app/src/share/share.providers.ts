import { EnvironmentProviders, Provider, signal } from '@angular/core';
import { Observable, of, throwError } from 'rxjs';

import { FileAttachmentService } from '../app/core/services/file-attachment.service';
import { FolderFiles } from '../app/features/web-preview/folder-files.service';
import { CommentPeopleService } from '../app/features/office/comments/comment-people.service';
import { TextCommentsService } from '../app/features/file-workspace/text-comments/text-comments.service';
import { JUPYTERLITE_URL, NOTEBOOK_INTERACTIVE_OUTPUT } from '../app/features/notebook/notebook-run.config';
import { DIAGRAMS_URL } from '../app/features/diagrams/diagram-site.config';
import { CommentAgentHooks, CommentAgentService } from '../app/features/office/comments/comment-agent.service';
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
    // No chat, no agent: nothing a viewer posts reaches anyone. A stand-in, so the app's workspace
    // and gallery services (which the real one reaches) are never made on the share site.
    { provide: CommentAgentService, useValue: noAgent() },
    // A page's picture is the owner's to draw: a visitor's view is never stored.
    {
      provide: FileAttachmentService,
      useValue: { hasPreview: () => of(true), putPreview: () => of(undefined) },
    },
    // The share's files stand in for the gallery's.
    { provide: FolderFiles, useExisting: ShareFolderFiles },
    // Notebooks show their saved outputs; nothing runs (the share site's policy would block a
    // script-driven output's frame, so it is not offered).
    { provide: JUPYTERLITE_URL, useValue: null },
    { provide: NOTEBOOK_INTERACTIVE_OUTPUT, useValue: false },
    // Diagrams are not shared (ADR 0x051 §1): no editor site to frame.
    { provide: DIAGRAMS_URL, useValue: null },
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

/** The agent comments service with no chat and no agent (ADR 0x051 §6). */
function noAgent(): Partial<CommentAgentService> {
  const hooks: CommentAgentHooks = {
    people: people => people,
    posted: () => undefined,
    mark: () => 0,
    saved: () => undefined,
    discard: () => undefined,
  };
  return {
    agent: signal(null),
    sent: signal(0),
    hooks: () => hooks,
    withAgent: people => people,
    watchAgentComments: () => undefined,
    agentCommented: () => undefined,
    posted: async () => false,
    register: () => () => undefined,
  };
}

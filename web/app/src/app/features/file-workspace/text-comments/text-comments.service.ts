import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, map } from 'rxjs';

import { environment } from '../../../../environments/environment';
import { CommentThread, DocComment } from '../../office/comments/comment-threads';

/**
 * Comments on text and code files (ADR 0x045), which have no place for them of their own: the
 * server keeps them beside the file and carries their anchors across changes made to it anywhere.
 */

/** A range of the file's text in UTF-16 code units (JavaScript string offsets); null for the whole file. */
export type TextCommentAnchor = { start: number; end: number } | null;
/** The element of a draw.io diagram a thread is on (ADR 0x049): the cell's id, on the page with id `page`. */
export interface DiagramCell {
  id: string;
  page?: string;
}
/** A thread on a range of the text, or the whole file; on a diagram, on one of its elements (`cell`, anchor null). */
export type TextCommentThread = CommentThread<TextCommentAnchor> & { cell?: DiagramCell };

export interface TextComments {
  threads: TextCommentThread[];
  /** The file content the anchors are placed on. */
  contentVersion: number;
  /** What a write sends back; 0 when the file has no comments yet. */
  revision: number;
}

interface ApiComment {
  id: string;
  author_id: string;
  author_name: string;
  created_at: string;
  text: string;
  mentions?: { id: string; name: string }[];
}

interface ApiThread {
  id: string;
  anchor: { start: number; end: number } | null;
  cell?: DiagramCell;
  quote: string;
  resolved: boolean;
  comments: ApiComment[];
}

interface ApiComments {
  threads: ApiThread[] | null;
  content_version: number;
  revision: number;
}

function fromApi(body: ApiComments): TextComments {
  return {
    contentVersion: body.content_version,
    revision: body.revision,
    threads: (body.threads ?? []).map(t => {
      const thread: TextCommentThread = {
        id: t.id,
        anchor: t.anchor ? { start: t.anchor.start, end: t.anchor.end } : null,
        quote: t.quote,
        comments: t.comments.map(c => {
          const comment: DocComment = { id: c.id, authorId: c.author_id, authorName: c.author_name, createdAt: c.created_at, text: c.text };
          if (c.mentions?.length) comment.mentions = c.mentions;
          return comment;
        }),
      };
      if (t.cell?.id) thread.cell = t.cell.page ? { id: t.cell.id, page: t.cell.page } : { id: t.cell.id };
      if (t.resolved) thread.resolved = true;
      return thread;
    }),
  };
}

function toApi(threads: readonly TextCommentThread[]): ApiThread[] {
  return threads.map(t => ({
    id: t.id,
    anchor: t.anchor,
    ...(t.cell ? { cell: t.cell } : {}),
    quote: t.quote ?? '',
    resolved: !!t.resolved,
    comments: t.comments.map(c => ({
      id: c.id,
      author_id: c.authorId,
      author_name: c.authorName,
      created_at: c.createdAt,
      text: c.text,
      ...(c.mentions?.length ? { mentions: c.mentions } : {}),
    })),
  }));
}

@Injectable({ providedIn: 'root' })
export class TextCommentsService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = environment.apiUrl;

  /** The file's threads, placed on its current content. */
  get(fileId: string): Observable<TextComments> {
    return this.http.get<ApiComments>(`${this.apiUrl}/file-attachment/${fileId}/comments`).pipe(map(fromApi));
  }

  /**
   * Stores the threads, placed on the text of contentVersion (412 when that is no longer the
   * file's content), over revision (409 when the comments changed since).
   */
  put(fileId: string, threads: readonly TextCommentThread[], contentVersion: number, revision: number): Observable<TextComments> {
    return this.http
      .put<ApiComments>(`${this.apiUrl}/file-attachment/${fileId}/comments`, {
        threads: toApi(threads),
        content_version: contentVersion,
        revision,
      })
      .pipe(map(fromApi));
  }
}

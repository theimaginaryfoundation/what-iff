/**
 * Comment threads, the same for every office editor that has them (ADR 0x045): who wrote what and
 * when, the people a comment mentions, and whether the thread is resolved. Where a thread sits in
 * the file is the editor's business, so the anchor is a type parameter: a text range in a
 * document, a slide (and a shape on it) in a deck.
 *
 * These are plain values and pure functions; an editor keeps its threads in a signal and replaces
 * the list on every change.
 */

/** Someone a comment can be written by or mention. */
export interface CommentPerson {
  /** `user:<id>` for a WhatIff user, `author:<name>` for someone only known from the file. */
  id: string;
  name: string;
}

export interface CommentMention {
  /** The person's id, as in CommentPerson. */
  id: string;
  name: string;
}

export interface DocComment {
  id: string;
  authorId: string;
  authorName: string;
  /** ISO 8601. */
  createdAt: string;
  /** Plain text; a mention is its `@Name` written into the text. */
  text: string;
  mentions?: CommentMention[];
}

export interface CommentThread<TAnchor> {
  /** The first comment's id. */
  id: string;
  anchor: TAnchor;
  /** What the thread is about, in words: the commented text, or the shape's name. */
  quote?: string;
  resolved?: boolean;
  /** The first comment, then the replies in order. */
  comments: DocComment[];
}

/** A comment not posted yet: its text and the people it mentions. */
export interface CommentDraft {
  text: string;
  mentions: CommentMention[];
}

/** A WhatIff user's id as a comment author. */
export function userPersonId(userId: string): string {
  return `user:${userId}`;
}

/**
 * The user id a file gives a WhatIff person under the WhatIff presence provider (ADR 0x046): a
 * user's id, or an agent's whole `agent:<id>`; null for someone the file only names.
 */
export function providerUserIdOf(personId: string): string | null {
  if (personId.startsWith('user:')) return personId.slice('user:'.length);
  if (personId.startsWith('agent:')) return personId;
  return null;
}

/** The person a file's WhatIff user id names: an agent by its `agent:<id>`, else a user. */
export function personIdFromProvider(userId: string): string {
  return userId.startsWith('agent:') ? userId : userPersonId(userId);
}

/** Someone known only by the name a file gives them. */
export function authorPersonId(name: string): string {
  return `author:${name}`;
}

/** The mentions still written into the text, once each. */
export function keptMentions(text: string, mentions: readonly CommentMention[]): CommentMention[] {
  const seen = new Set<string>();
  return mentions.filter(m => {
    if (seen.has(m.id) || !text.includes(`@${m.name}`)) return false;
    seen.add(m.id);
    return true;
  });
}

/** A new comment by `author` from a draft. */
export function newComment(id: string, author: CommentPerson, draft: CommentDraft, now = new Date()): DocComment {
  const text = draft.text.trim();
  const comment: DocComment = { id, authorId: author.id, authorName: author.name, createdAt: now.toISOString(), text };
  const mentions = keptMentions(text, draft.mentions);
  if (mentions.length) comment.mentions = mentions;
  return comment;
}

export function addReply<T>(threads: readonly CommentThread<T>[], threadId: string, comment: DocComment): CommentThread<T>[] {
  return threads.map(t => (t.id === threadId ? { ...t, comments: [...t.comments, comment] } : t));
}

export function editComment<T>(
  threads: readonly CommentThread<T>[],
  threadId: string,
  commentId: string,
  draft: CommentDraft,
): CommentThread<T>[] {
  const text = draft.text.trim();
  const mentions = keptMentions(text, draft.mentions);
  return threads.map(t =>
    t.id !== threadId
      ? t
      : {
          ...t,
          comments: t.comments.map(c => {
            if (c.id !== commentId) return c;
            const edited: DocComment = { ...c, text };
            if (mentions.length) edited.mentions = mentions;
            else delete edited.mentions;
            return edited;
          }),
        },
  );
}

/** Removes one reply, or the whole thread when it is the first comment. */
export function removeComment<T>(threads: readonly CommentThread<T>[], threadId: string, commentId: string): CommentThread<T>[] {
  if (threadId === commentId) return threads.filter(t => t.id !== threadId);
  return threads.map(t => (t.id === threadId ? { ...t, comments: t.comments.filter(c => c.id !== commentId) } : t));
}

export function setResolved<T>(threads: readonly CommentThread<T>[], threadId: string, resolved: boolean): CommentThread<T>[] {
  return threads.map(t => (t.id === threadId ? { ...t, resolved } : t));
}

/** Everyone who wrote a comment, for the mention list. */
export function commentAuthors(threads: readonly CommentThread<unknown>[]): CommentPerson[] {
  const people = new Map<string, CommentPerson>();
  for (const t of threads)
    for (const c of t.comments) if (!people.has(c.authorId)) people.set(c.authorId, { id: c.authorId, name: c.authorName });
  return [...people.values()];
}

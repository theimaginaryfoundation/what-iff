import { of, throwError } from 'rxjs';

import { TextCommentThread, TextComments } from './text-comments.service';

/**
 * A TextCommentsService double: keeps one file's comments and records writes. A write over another
 * revision than the stored one is refused with 409, as the server does; `failNext` refuses the next
 * write with that status.
 */
export function fakeTextComments(initial: TextComments = { threads: [], contentVersion: 1, revision: 0 }) {
  const state = {
    stored: initial,
    puts: [] as { fileId: string; threads: TextCommentThread[]; contentVersion: number; revision: number }[],
    failNext: null as number | null,
  };
  return {
    state,
    get: () => of(state.stored),
    put: (fileId: string, threads: readonly TextCommentThread[], contentVersion: number, revision: number) => {
      state.puts.push({ fileId, threads: [...threads], contentVersion, revision });
      const fail = state.failNext ?? (revision !== state.stored.revision ? 409 : null);
      state.failNext = null;
      if (fail) return throwError(() => ({ status: fail }));
      state.stored = { threads: [...threads], contentVersion, revision: revision + 1 };
      return of(state.stored);
    },
  };
}

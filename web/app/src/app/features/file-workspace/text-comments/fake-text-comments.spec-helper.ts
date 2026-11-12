import { of } from 'rxjs';

import { TextCommentThread, TextComments } from './text-comments.service';

/** A TextCommentsService double: keeps one file's comments and records writes. */
export function fakeTextComments(initial: TextComments = { threads: [], contentVersion: 1, revision: 0 }) {
  const state = {
    stored: initial,
    puts: [] as { fileId: string; threads: TextCommentThread[]; contentVersion: number; revision: number }[],
  };
  return {
    state,
    get: () => of(state.stored),
    put: (fileId: string, threads: readonly TextCommentThread[], contentVersion: number, revision: number) => {
      state.puts.push({ fileId, threads: [...threads], contentVersion, revision });
      state.stored = { threads: [...threads], contentVersion, revision: revision + 1 };
      return of(state.stored);
    },
  };
}

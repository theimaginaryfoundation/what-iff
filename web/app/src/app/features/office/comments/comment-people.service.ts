import { Injectable, Signal, computed, inject } from '@angular/core';

import { AuthService } from '../../../core/services/auth.service';
import { CommentPerson, userPersonId } from './comment-threads';

/**
 * Who is commenting (ADR 0x045): the signed-in user, by name, so comments say who wrote them and a
 * file saved here records them as the author.
 */
@Injectable({ providedIn: 'root' })
export class CommentPeopleService {
  private readonly auth = inject(AuthService);

  readonly me: Signal<CommentPerson> = computed(() => {
    const user = this.auth.currentUser();
    if (!user) return { id: userPersonId('anonymous'), name: 'You' };
    const name = [user.first_name, user.last_name].filter(Boolean).join(' ').trim() || user.username || user.email;
    return { id: userPersonId(user.id), name };
  });
}

import { Injectable, signal } from '@angular/core';
import { Subject } from 'rxjs';

import { Personality } from '../models/personality.model';

@Injectable({ providedIn: 'root' })
export class GeneratePersonalityModalService {
  readonly open = signal(false);

  private readonly createdSubject = new Subject<Personality>();
  /** Emits when a personality is created via the generate modal flow. */
  readonly personalityCreated$ = this.createdSubject.asObservable();

  show(): void {
    this.open.set(true);
  }

  hide(): void {
    this.open.set(false);
  }

  notifyPersonalityCreated(personality: Personality): void {
    this.createdSubject.next(personality);
  }
}

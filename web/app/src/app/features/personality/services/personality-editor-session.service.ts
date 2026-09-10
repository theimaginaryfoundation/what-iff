import { Injectable, signal } from '@angular/core';

import {
  Personality,
  PersonalityThumbnailCircle,
} from '../../../core/models/personality.model';

export interface PersonalityEditDraft {
  name: string;
  system_prompt: string;
  scratchpad: string;
  scratchpad_update_prompt: string;
  auto_pin_memories: boolean;
  accent_color: string | null;
  cover_image_id: string | null;
  thumbnail_circle: PersonalityThumbnailCircle | null;
}

const DEFAULT_THUMBNAIL_CIRCLE: PersonalityThumbnailCircle = { cx: 0.5, cy: 0.42, r: 0.34 };

/**
 * Holds one unsaved personality draft while users move between compact and
 * expanded editor surfaces. A draft is cleared after an explicit compact-editor
 * close or deletion; expanded saves keep it available for a subsequent contract.
 */
@Injectable({ providedIn: 'root' })
export class PersonalityEditorSessionService {
  readonly personalityId = signal<string | null>(null);
  readonly draft = signal<PersonalityEditDraft | null>(null);

  begin(personality: Personality): void {
    if (this.personalityId() === personality.id && this.draft()) {
      return;
    }
    this.personalityId.set(personality.id);
    this.draft.set(this.draftFromPersonality(personality));
  }

  update<K extends keyof PersonalityEditDraft>(key: K, value: PersonalityEditDraft[K]): void {
    this.draft.update(current => current ? { ...current, [key]: value } : current);
  }

  commit(personality: Personality): void {
    this.personalityId.set(personality.id);
    this.draft.set(this.draftFromPersonality(personality));
  }

  clear(personalityId?: string): void {
    if (personalityId && this.personalityId() !== personalityId) {
      return;
    }
    this.personalityId.set(null);
    this.draft.set(null);
  }

  private draftFromPersonality(personality: Personality): PersonalityEditDraft {
    return {
      name: personality.name,
      system_prompt: personality.system_prompt,
      scratchpad: personality.scratchpad ?? '',
      scratchpad_update_prompt: personality.scratchpad_update_prompt ?? '',
      auto_pin_memories: personality.auto_pin_memories,
      accent_color: personality.accent_color ?? null,
      cover_image_id: personality.cover_image_id ?? null,
      thumbnail_circle: personality.thumbnail_circle ?? DEFAULT_THUMBNAIL_CIRCLE,
    };
  }
}

import { TestBed } from '@angular/core/testing';

import { Personality } from '../../../core/models/personality.model';
import { PersonalityEditorSessionService } from './personality-editor-session.service';

const personality: Personality = {
  id: 'p-1',
  name: 'Vera',
  system_prompt: 'Prompt',
  scratchpad: 'Scratchpad',
  auto_pin_memories: false,
  expressions_enabled: true,
  image_style: 'auto',
  cover_image_id: null,
  cover_image_url: null,
  accent_color: '#C2572A',
  thumbnail_circle: { cx: 0.5, cy: 0.42, r: 0.34 },
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
  stats: { chat_count: 0, last_used_at: null },
};

describe('PersonalityEditorSessionService', () => {
  let service: PersonalityEditorSessionService;

  beforeEach(() => {
    TestBed.configureTestingModule({});
    service = TestBed.inject(PersonalityEditorSessionService);
  });

  it('preserves an existing draft when the same personality opens another editor surface', () => {
    service.begin(personality);
    service.update('name', 'Unsaved Vera');

    service.begin(personality);

    expect(service.draft()?.name).toBe('Unsaved Vera');
  });

  it('replaces the draft when a different personality opens', () => {
    service.begin(personality);
    service.update('name', 'Unsaved Vera');

    service.begin({ ...personality, id: 'p-2', name: 'Filbolt' });

    expect(service.personalityId()).toBe('p-2');
    expect(service.draft()?.name).toBe('Filbolt');
  });

  it('clears only the matching personality draft', () => {
    service.begin(personality);

    service.clear('other-personality');
    expect(service.draft()).not.toBeNull();

    service.clear('p-1');
    expect(service.draft()).toBeNull();
  });
});

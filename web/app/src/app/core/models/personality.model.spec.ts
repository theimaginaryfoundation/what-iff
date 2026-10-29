import { buildPersonalityUpdateRequest } from './personality.model';
import { makeTestPersonality } from './personality.test-fixtures';

describe('buildPersonalityUpdateRequest', () => {
  const personality = makeTestPersonality({
    cover_image_id: 'cover-1',
    accent_color: '#A1B2C3',
    thumbnail_circle: { cx: 0.5, cy: 0.4, r: 0.3 },
    expressions_enabled: true,
    image_style: 'watercolor',
  });

  it('keeps every styling field the PUT would otherwise clear', () => {
    const request = buildPersonalityUpdateRequest(personality, { expressions_enabled: false });

    expect(request.expressions_enabled).toBe(false);
    expect(request.cover_image_id).toBe('cover-1');
    expect(request.accent_color).toBe('#A1B2C3');
    expect(request.thumbnail_circle).toEqual({ cx: 0.5, cy: 0.4, r: 0.3 });
    expect(request.image_style).toBe('watercolor');
  });

  it('leaves the turn settings out unless an override names them, so the server keeps them', () => {
    const withSettings = makeTestPersonality({ reasoning_effort: 'xhigh' as never, agent_loop: 'advanced' });
    const request = buildPersonalityUpdateRequest(withSettings, { name: 'Renamed' });
    expect('reasoning_effort' in request).toBe(false);
    expect('agent_loop' in request).toBe(false);

    const change = buildPersonalityUpdateRequest(withSettings, { agent_loop: null });
    expect(change.agent_loop).toBeNull();
    expect('reasoning_effort' in change).toBe(false);
  });

  it('lets an override clear a field explicitly', () => {
    expect(buildPersonalityUpdateRequest(personality, { accent_color: null }).accent_color).toBeNull();
  });
});

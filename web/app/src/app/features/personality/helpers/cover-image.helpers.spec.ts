import { PersonalityExpression } from '../../../core/models/personality.model';
import { personalityCoverUrl } from './cover-image.helpers';

function makeExpression(key: string, imageUrl: string | null): PersonalityExpression {
  return {
    expression_key: key,
    label: null,
    image_id: imageUrl ? 'img-' + key : null,
    image_url: imageUrl,
    created_at: '2026-04-28T00:00:00Z',
    updated_at: '2026-04-28T00:00:00Z',
  };
}

describe('personalityCoverUrl', () => {
  const getImageUrl = (id: string, size: 'thumbnail' | 'full') => `/api/image-gallery/${id}?size=${size}`;

  it('returns null when nothing is available', () => {
    expect(personalityCoverUrl(null, null)).toBeNull();
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, [])).toBeNull();
  });

  it('prefers an explicit cover_image_id when a resolver is available', () => {
    const url = personalityCoverUrl(
      { cover_image_id: 'cover-id', cover_image_url: '/cover.png' },
      [makeExpression('happy', '/happy.png')],
      getImageUrl,
    );
    expect(url).toBe('/api/image-gallery/cover-id?size=thumbnail');
  });

  it('prefers an explicit cover_image_url', () => {
    const url = personalityCoverUrl(
      { cover_image_id: null, cover_image_url: '/cover.png' },
      [makeExpression('happy', '/happy.png')],
    );
    expect(url).toBe('/cover.png');
  });

  it('falls back to a preferred expression key', () => {
    const expressions = [makeExpression('happy', '/happy.png'), makeExpression('sad', '/sad.png')];
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, expressions)).toBe('/happy.png');
  });

  it('prefers expression image_id when a resolver is available', () => {
    const expressions = [makeExpression('happy', '/happy.png'), makeExpression('sad', '/sad.png')];
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, expressions, getImageUrl))
      .toBe('/api/image-gallery/img-happy?size=thumbnail');
  });

  it('falls back to the first available expression image alphabetically', () => {
    const expressions = [makeExpression('worried', '/worried.png'), makeExpression('argh', '/argh.png')];
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, expressions)).toBe('/argh.png');
  });

  it('skips expressions without image URLs', () => {
    const expressions = [
      makeExpression('happy', null),
      makeExpression('sad', '/sad.png'),
    ];
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, expressions)).toBe('/sad.png');
  });

  it('uses content > happy ordering when both are present', () => {
    const expressions = [makeExpression('happy', '/happy.png'), makeExpression('content', '/content.png')];
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, expressions)).toBe('/happy.png');
  });

  it('prefers default when present', () => {
    const expressions = [
      makeExpression('happy', '/happy.png'),
      makeExpression('default', '/default.png'),
    ];
    expect(personalityCoverUrl({ cover_image_id: null, cover_image_url: null }, expressions)).toBe('/default.png');
  });
});

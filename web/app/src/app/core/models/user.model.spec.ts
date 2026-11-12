import { UserPreferences, imageEditingUnavailableReason } from './user.model';

describe('imageEditingUnavailableReason', () => {
  const prefs = (overrides: Partial<UserPreferences>) => overrides as UserPreferences;

  it('says nothing before preferences load, or when the vendor in use can edit', () => {
    expect(imageEditingUnavailableReason(null)).toBeNull();
    expect(
      imageEditingUnavailableReason(prefs({ image_generation_providers: ['openai', 'zai'], image_generation_provider: 'openai' })),
    ).toBeNull();
    // A pick that is no longer offered falls back to the first offered, as the server does.
    expect(imageEditingUnavailableReason(prefs({ image_generation_providers: ['google'], image_generation_provider: 'zai' }))).toBeNull();
  });

  it('names a vendor that cannot edit, and where to change it', () => {
    const reason = imageEditingUnavailableReason(
      prefs({ image_generation_providers: ['openai', 'qwen'], image_generation_provider: 'qwen' }),
    );
    expect(reason).toContain('Qwen (Qwen-Image)');
    expect(reason).toContain('Settings → Image generation');
  });

  it('says when image generation is not set up at all', () => {
    expect(imageEditingUnavailableReason(prefs({ image_generation_providers: [] }))).toContain('not set up');
  });
});

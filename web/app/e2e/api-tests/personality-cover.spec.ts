import { test, expect } from './fixtures';
import { createPersonality, deletePersonality, getPersonality, updatePersonality, type Personality, ApiError } from '../sdk/client';
import { shortId } from '../fixtures/unique';

/**
 * The cover-portrait fields on a personality (`cover_image_id` /
 * `cover_image_url`).
 *
 * Ported from the Newman regression suite's "13 PersonalityCover" folder.
 * As with expressions, the step that assigns a *real* cover needs an
 * uploaded gallery image and therefore a live provider, so it lives in the
 * private overlay suite — see personality-expressions.spec.ts for the full
 * reasoning. What survives here is the contract that does not need one: a
 * personality starts uncovered, an unknown image ID is refused, and null
 * clears rather than being ignored.
 */

test.describe('personality cover', () => {
  let personality: Personality;
  let name: string;

  test.beforeEach(async ({ apiClient }) => {
    name = `e2e-api-cover ${shortId()}`;
    personality = await createPersonality(apiClient, { name, systemPrompt: 'Cover regression personality.' });
  });

  test.afterEach(async ({ apiClient }) => {
    await deletePersonality(apiClient, personality.id!);
  });

  test('a new personality has no cover', async ({ apiClient }) => {
    expect(personality.cover_image_id).toBeNull();
    expect(personality.cover_image_url).toBeNull();

    const fetched = await getPersonality(apiClient, personality.id!);
    expect(fetched.cover_image_id).toBeNull();
    expect(fetched.cover_image_url).toBeNull();
  });

  test('an image the user does not own is refused', async ({ apiClient }) => {
    await expect(
      updatePersonality(apiClient, personality.id!, {
        name,
        systemPrompt: 'Cover regression personality.',
        // Nil UUID: a well-formed UUID that owns nothing, so this reaches the
        // ownership check rather than failing request validation.
        coverImageId: '00000000-0000-0000-0000-000000000000',
      }),
    ).rejects.toMatchObject({ status: 404 } satisfies Partial<ApiError>);

    // The rejected write must not have left a partial cover behind.
    const after = await getPersonality(apiClient, personality.id!);
    expect(after.cover_image_id).toBeNull();
  });

  test('sending null clears the cover', async ({ apiClient }) => {
    const cleared = await updatePersonality(apiClient, personality.id!, {
      name,
      systemPrompt: 'Cover regression personality.',
      coverImageId: null,
    });

    expect(cleared.cover_image_id).toBeNull();
    expect(cleared.cover_image_url).toBeNull();
  });
});

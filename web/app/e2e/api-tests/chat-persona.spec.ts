import { test, expect, createSecondUser } from './fixtures';
import {
  createChat,
  createPersonality,
  deleteChat,
  deletePersonality,
  getChat,
  getPersonality,
  listModels,
  type Personality,
  ApiError,
} from '../sdk/client';
import { shortId, uniqueId } from '../fixtures/unique';

/**
 * Creating a chat with a persona and model attached, and the ownership
 * checks on both references.
 *
 * Ported from the Newman regression suite's "14 ChatCreateWithPersona"
 * folder. Both rejection cases are 404 rather than 403: the backend scopes
 * personalities and models to the requesting user at lookup, so "not yours"
 * and "does not exist" are indistinguishable by design.
 */

test.describe('chat creation with a persona', () => {
  let personality: Personality;

  test.beforeEach(async ({ apiClient }) => {
    personality = await createPersonality(apiClient, {
      name: `e2e-api-persona ${shortId()}`,
      systemPrompt: 'Persona regression personality.',
    });
  });

  test.afterEach(async ({ apiClient }) => {
    await deletePersonality(apiClient, personality.id!);
  });

  test('a chat keeps the persona and model it was created with', async ({ apiClient }) => {
    const model = (await listModels(apiClient))[0]!;

    const chat = await createChat(apiClient, {
      name: `e2e-api-persona-chat ${shortId()}`,
      personalityId: personality.id!,
      modelId: model.id,
    });
    try {
      expect(chat.personality_id).toBe(personality.id);
      expect(chat.model_id).toBe(model.id);

      // Read back rather than trusting the create response echo.
      const fetched = await getChat(apiClient, chat.id!);
      expect(fetched.personality_id).toBe(personality.id);
      expect(fetched.model_id).toBe(model.id);
    } finally {
      await deleteChat(apiClient, chat.id!);
    }
  });

  test('a personality ID that matches nothing is rejected', async ({ apiClient }) => {
    // A random UUID, deliberately not the nil UUID: the API documents
    // `00000000-...` as the sentinel meaning "clear the personality" (see
    // PATCH /chat/{id} in openapi.yaml), so sending it is accepted and the
    // chat is created with no persona. The Newman suite this was ported from
    // used the nil UUID here and so asserted 404 against the sentinel rather
    // than against a lookup miss.
    await expect(
      createChat(apiClient, { name: 'e2e-api-persona-foreign', personalityId: uniqueId() }),
    ).rejects.toMatchObject({ status: 404 } satisfies Partial<ApiError>);
  });

  test('the nil UUID is a sentinel that resolves to the default persona', async ({ apiClient }) => {
    const chat = await createChat(apiClient, {
      name: 'e2e-api-persona-nil',
      personalityId: '00000000-0000-0000-0000-000000000000',
    });
    try {
      // Not rejected, and not stored as-is: the nil UUID resolves to the
      // account's default personality, which is a real, fetchable row.
      expect(chat.personality_id).not.toBe('00000000-0000-0000-0000-000000000000');
      const resolved = await getPersonality(apiClient, chat.personality_id!);
      expect(resolved.id).toBe(chat.personality_id);
    } finally {
      await deleteChat(apiClient, chat.id!);
    }
  });

  test("another user's personality cannot be attached to a chat", async ({ apiClient }) => {
    const other = await createSecondUser();
    try {
      const theirs = await createPersonality(other.apiClient, {
        name: `e2e-api-persona-other ${shortId()}`,
        systemPrompt: 'Other user personality.',
      });

      await expect(
        createChat(apiClient, { name: 'e2e-api-persona-cross-user', personalityId: theirs.id! }),
      ).rejects.toMatchObject({ status: 404 } satisfies Partial<ApiError>);
    } finally {
      await other.cleanup();
    }
  });
});

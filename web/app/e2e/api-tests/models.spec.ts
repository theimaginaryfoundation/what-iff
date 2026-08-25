import { test, expect } from './fixtures';
import { listModels, createChat, deleteChat, updateChat, getChat, ApiError } from '../sdk/client';
import { uniqueId } from '../fixtures/unique';

/**
 * GET /model and the model half of chat assignment, straight against the API.
 *
 * Ported from the Newman regression suite's "01 ModelAndPersonality / List
 * models" and the deterministic half of "07 ModelSwitch" (that suite's
 * scenario-contract.md, steps 4 and 19). What
 * that suite additionally asserted — that a *reply generated after* the
 * switch reports the new model in `generation_model` — needs a real provider
 * and lives in the private overlay suite instead; under the mock adapter
 * every reply is the same in-process echo.
 *
 * The provider mix is deployment configuration, not an API contract, so this
 * asserts shape and that a listed model is actually assignable rather than
 * that any particular provider is seeded.
 */

test.describe('models', () => {
  test('lists at least one selectable model', async ({ apiClient }) => {
    const models = await listModels(apiClient);

    expect(models.length).toBeGreaterThan(0);
    for (const model of models) {
      expect(model.id).toBeTruthy();
      expect(model.name).not.toBe('');
      expect(model.display_name).not.toBe('');
      expect(typeof model.tool_support).toBe('boolean');
      // Soft-deleted models are filtered server-side; the list is what a user
      // may pick from, so anything returned here must be assignable below.
      expect(model.deleted).toBe(false);
    }
  });

  test('a listed model can be assigned at creation and switched afterwards', async ({ apiClient }) => {
    const models = await listModels(apiClient);
    const first = models[0]!;
    // Falls back to the only model when the environment seeds just one — the
    // create-then-switch path is still exercised, it just lands on the same ID.
    const second = models[1] ?? first;

    const chat = await createChat(apiClient, { name: 'e2e-api-models', modelId: first.id });
    try {
      expect(chat.model_id).toBe(first.id);

      const switched = await updateChat(apiClient, chat.id!, { modelId: second.id });
      expect(switched.model_id).toBe(second.id);

      const reread = await getChat(apiClient, chat.id!);
      expect(reread.model_id).toBe(second.id);
    } finally {
      await deleteChat(apiClient, chat.id!);
    }
  });

  test('a model ID that matches nothing is rejected', async ({ apiClient }) => {
    // A random UUID rather than the nil UUID, which the API treats as "no
    // model given" and answers by falling back to the default model — see
    // the matching note in chat-persona.spec.ts.
    await expect(
      createChat(apiClient, { name: 'e2e-api-models-foreign', modelId: uniqueId() }),
    ).rejects.toMatchObject({ status: 404 } satisfies Partial<ApiError>);
  });
});

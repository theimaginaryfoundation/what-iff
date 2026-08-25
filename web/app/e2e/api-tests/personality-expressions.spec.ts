import { test, expect, createSecondUser } from './fixtures';
import {
  createPersonality,
  deletePersonality,
  deletePersonalityExpression,
  listPersonalityExpressions,
  upsertPersonalityExpression,
  type Personality,
} from '../sdk/client';
import { shortId } from '../fixtures/unique';

/**
 * PUT/GET/DELETE /personality/{id}/expressions — the user-defined expression
 * slots a personality can carry.
 *
 * Ported from the Newman regression suite's "12 PersonalityExpressions"
 * folder, minus the steps that assign a *real* image: an expression's
 * `image_id` has to be a gallery attachment, and creating one goes through
 * POST /personality/{id}/file-attachment, which uploads to the provider's
 * files API. Under `LLM_BACKEND=mock` provider egress is denied outright
 * (internal/agent/provider/deny_transport.go), so that upload answers 500
 * and the image-bearing assertions live in the private overlay suite, which
 * runs against a real provider. Everything reachable without an upload —
 * upsert semantics, listing, deletion, ownership — is here.
 */

test.describe('personality expressions', () => {
  let personality: Personality;
  let runId: string;

  test.beforeEach(async ({ apiClient }) => {
    runId = shortId();
    personality = await createPersonality(apiClient, {
      name: `e2e-api-expressions ${runId}`,
      systemPrompt: 'Expression regression personality.',
    });
  });

  test.afterEach(async ({ apiClient }) => {
    await deletePersonality(apiClient, personality.id!);
  });

  test('a new personality starts with no expression slots', async ({ apiClient }) => {
    expect(await listPersonalityExpressions(apiClient, personality.id!)).toStrictEqual([]);
  });

  test('an expression key can be created without an image', async ({ apiClient }) => {
    const created = await upsertPersonalityExpression(apiClient, personality.id!, 'excited', {
      label: `Excited ${runId}`,
    });

    expect(created.expression_key).toBe('excited');
    expect(created.label).toBe(`Excited ${runId}`);
    expect(created.image_id).toBeNull();
    expect(created.image_url).toBeNull();

    const listed = await listPersonalityExpressions(apiClient, personality.id!);
    expect(listed).toHaveLength(1);
    expect(listed[0]!.expression_key).toBe('excited');
  });

  test('the same key upserts rather than duplicating', async ({ apiClient }) => {
    await upsertPersonalityExpression(apiClient, personality.id!, 'happy', { label: `Happy ${runId}` });
    const updated = await upsertPersonalityExpression(apiClient, personality.id!, 'happy', {
      label: `Very Happy ${runId}`,
    });

    expect(updated.label).toBe(`Very Happy ${runId}`);

    const listed = await listPersonalityExpressions(apiClient, personality.id!);
    expect(listed).toHaveLength(1);
    expect(listed[0]!.label).toBe(`Very Happy ${runId}`);
  });

  test('an omitted field is left alone and an explicit null clears it', async ({ apiClient }) => {
    await upsertPersonalityExpression(apiClient, personality.id!, 'happy', { label: `Happy ${runId}` });

    // `image_id` sent, `label` omitted entirely — the stored label must
    // survive. This is the same merge rule the private suite asserts in the
    // other direction (label sent, image preserved), which needs an uploaded
    // image to be observable. A body with *neither* field is rejected 400,
    // so the omission has to be paired with something.
    const untouched = await upsertPersonalityExpression(apiClient, personality.id!, 'happy', { imageId: null });
    expect(untouched.label).toBe(`Happy ${runId}`);

    const cleared = await upsertPersonalityExpression(apiClient, personality.id!, 'happy', { label: null });
    expect(cleared.label).toBeNull();
  });

  test('deleting one key leaves the others in place', async ({ apiClient }) => {
    await upsertPersonalityExpression(apiClient, personality.id!, 'happy', { label: `Happy ${runId}` });
    await upsertPersonalityExpression(apiClient, personality.id!, 'excited', { label: `Excited ${runId}` });
    expect(await listPersonalityExpressions(apiClient, personality.id!)).toHaveLength(2);

    await deletePersonalityExpression(apiClient, personality.id!, 'happy');

    const remaining = await listPersonalityExpressions(apiClient, personality.id!);
    expect(remaining).toHaveLength(1);
    expect(remaining[0]!.expression_key).toBe('excited');
  });

  test("another user cannot read or write this personality's expressions", async () => {
    const other = await createSecondUser();
    try {
      const read = await other.apiClient.GET('/personality/{id}/expressions', {
        params: { path: { id: personality.id! } },
      });
      expect(read.response.status).toBe(404);

      const write = await other.apiClient.PUT('/personality/{id}/expressions/{expression_key}', {
        params: { path: { id: personality.id!, expression_key: 'happy' } },
        body: { label: 'stolen' },
      });
      expect(write.response.status).toBe(404);
    } finally {
      await other.cleanup();
    }
  });
});

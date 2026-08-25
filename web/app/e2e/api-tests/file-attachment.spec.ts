import { test, expect } from './fixtures';
import { createApiClient, createPersonality, deletePersonality, listFileAttachments, type Personality } from '../sdk/client';
import { shortId } from '../fixtures/unique';

/**
 * `POST /personality/{id}/file-attachment` — the ways it refuses an upload,
 * and the listing endpoint that shows nothing was created.
 *
 * **Why there is no successful upload here.** The handler streams every file
 * to the vendor Files API through `handlerutils.UploadFileAttachment` before
 * it writes anything, and `internal/server/server.go` gives every provider
 * client the deny-egress transport whenever `LLM_BACKEND != "vendor"` — under
 * both `mock` and `local`, so the happy path 500s on every backend this
 * suite runs against. See the "Constraints" section of TEST_PLAN.md.
 *
 * Every refusal below, by contrast, is settled *before* that call —
 * `ParseMultipartForm`, `FormFile`, `utils.GetFileType` and the uuid parse
 * all run first — so each answers identically on a mock, local or
 * vendor-backed backend. That is what makes them ordinary mock-suite tests
 * rather than something needing a real key.
 */

/** The API's cap, from `maxUploadMb` in internal/handlers/handlerutils/fileattachment.go. */
const MAX_UPLOAD_BYTES = 30 * 1024 * 1024;

/** A real, valid 1x1 PNG — a supported type, so a refusal cannot be blamed on the bytes. */
const TINY_PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==',
  'base64',
);

/** Builds the multipart body the endpoint expects, so each test varies one thing about it. */
function attachmentBody(filename: string, contentType: string, bytes: Buffer): () => FormData {
  return () => {
    const form = new FormData();
    form.append('attachment', new Blob([new Uint8Array(bytes).buffer], { type: contentType }), filename);
    return form;
  };
}

test.describe('personality file attachment uploads', () => {
  let personality: Personality;

  test.beforeEach(async ({ apiClient }) => {
    personality = await createPersonality(apiClient, {
      name: `e2e-upload ${shortId()}`,
      systemPrompt: 'Upload refusal fixture.',
    });
  });

  test.afterEach(async ({ apiClient }) => {
    await deletePersonality(apiClient, personality.id!);
  });

  test('refuses a file type the API has no entry for', async ({ apiClient }) => {
    // `.webp` is the one that matters in practice: the web client's own MIME
    // allowlist accepts image/webp, so this is reachable from the UI rather
    // than only from a hand-written request.
    const { response } = await apiClient.POST('/personality/{id}/file-attachment', {
      params: { path: { id: personality.id! } },
      body: { attachment: 'cover.webp' },
      bodySerializer: attachmentBody('cover.webp', 'image/webp', Buffer.from('RIFF____WEBPVP8 ')),
    });

    expect(response.status).toBe(400);
    expect(await listFileAttachments(apiClient)).toHaveLength(0);
  });

  test('refuses a request with no file part', async ({ apiClient }) => {
    const { response } = await apiClient.POST('/personality/{id}/file-attachment', {
      params: { path: { id: personality.id! } },
      body: { attachment: '' },
      bodySerializer: () => {
        // A well-formed multipart body whose only field is not the one
        // `FormFile("attachment")` looks for.
        const form = new FormData();
        form.append('title', 'no file here');
        return form;
      },
    });

    expect(response.status).toBe(400);
  });

  test('refuses a file over the size cap', async ({ apiClient }) => {
    // Zero bytes, not a real image: `MaxBytesReader` trips on the length
    // before anything reads the content, so valid PNG structure would prove
    // nothing and cost 30MB to carry around.
    const oversized = Buffer.alloc(MAX_UPLOAD_BYTES + 1);

    const { response } = await apiClient.POST('/personality/{id}/file-attachment', {
      params: { path: { id: personality.id! } },
      body: { attachment: 'oversized.png' },
      bodySerializer: attachmentBody('oversized.png', 'image/png', oversized),
    });

    expect(response.status).toBe(400);
    expect(await listFileAttachments(apiClient)).toHaveLength(0);
  });

  test('refuses a malformed personality id', async ({ apiClient }) => {
    const { response } = await apiClient.POST('/personality/{id}/file-attachment', {
      params: { path: { id: 'not-a-uuid' } },
      body: { attachment: 'tiny.png' },
      bodySerializer: attachmentBody('tiny.png', 'image/png', TINY_PNG),
    });

    expect(response.status).toBe(400);
  });

  test('refuses an unauthenticated upload', async () => {
    const anonymous = createApiClient();

    const { response } = await anonymous.POST('/personality/{id}/file-attachment', {
      params: { path: { id: personality.id! } },
      body: { attachment: 'tiny.png' },
      bodySerializer: attachmentBody('tiny.png', 'image/png', TINY_PNG),
    });

    expect(response.status).toBe(401);
  });
});

test.describe('file attachment listing', () => {
  test('a fresh account has no attachments', async ({ apiClient }) => {
    // Thin on its own, but it is what makes the "nothing was created"
    // assertions in the refusal tests above mean something: it shows an empty
    // list is this endpoint reporting truthfully, not the endpoint being
    // broken or absent. It was absent from the generated SDK types until the
    // openapi.yaml path item was un-nested.
    expect(await listFileAttachments(apiClient)).toHaveLength(0);
  });
});

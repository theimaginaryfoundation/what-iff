import { AccountArchiveService } from './account-archive.service';

/** Builds an in-memory .zip File from a map of path → text content (+ an optional memories.zip). */
async function makeZip(files: Record<string, string>, opts: { memoriesZip?: boolean } = {}): Promise<File> {
  const { default: JSZip } = await import('jszip');
  const zip = new JSZip();
  for (const [path, content] of Object.entries(files)) {
    zip.file(path, content);
  }
  if (opts.memoriesZip) {
    zip.file('memories.zip', new Uint8Array([1, 2, 3]));
  }
  const blob = await zip.generateAsync({ type: 'blob' });
  return new File([blob], 'export.zip', { type: 'application/zip' });
}

describe('AccountArchiveService', () => {
  const svc = new AccountArchiveService();
  const p1 = '11111111-1111-1111-1111-111111111111';
  const c1 = '22222222-2222-2222-2222-222222222222';

  it('enumerates personalities and threads and detects memories from a WhatIff export', async () => {
    const file = await makeZip(
      {
        'manifest.json': JSON.stringify({ schema_version: 1, counts: {} }),
        [`personalities/${p1}/personality.json`]: JSON.stringify({ whatiff_personality_id: p1, name: 'Aria' }),
        'conversations.json': JSON.stringify([{ uuid: c1, name: 'A chat' }]),
      },
      { memoriesZip: true },
    );

    const contents = await svc.inspect(file);
    expect(contents.personalities).toEqual([{ id: p1, name: 'Aria' }]);
    expect(contents.conversations).toEqual([{ id: c1, title: 'A chat' }]);
    expect(contents.hasMemories).toBe(true);
  });

  it('falls back to the archive dir for a personality id and defaults an empty title', async () => {
    const file = await makeZip({
      'manifest.json': JSON.stringify({ schema_version: 1 }),
      [`personalities/${p1}/personality.json`]: JSON.stringify({ name: 'NoIdField' }),
      'conversations.json': JSON.stringify([{ uuid: c1, name: '' }]),
    });

    const contents = await svc.inspect(file);
    expect(contents.personalities).toEqual([{ id: p1, name: 'NoIdField' }]);
    expect(contents.conversations).toEqual([{ id: c1, title: 'Untitled conversation' }]);
    expect(contents.hasMemories).toBe(false);
  });

  it('rejects a file without a manifest (not a WhatIff export)', async () => {
    const file = await makeZip({ 'conversations.json': '[]' });
    await expect(svc.inspect(file)).rejects.toThrow(/does not look like a WhatIff export/);
  });

  it('rejects an export with nothing to import', async () => {
    const file = await makeZip({ 'manifest.json': JSON.stringify({ schema_version: 1 }) });
    await expect(svc.inspect(file)).rejects.toThrow(/no conversations, personalities, or memories/);
  });
});

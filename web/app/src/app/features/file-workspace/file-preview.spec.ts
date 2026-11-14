import { of, throwError } from 'rxjs';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachment, PreviewSource } from '../../core/models/file-attachment.model';
import { FilePreview } from './file-preview';

const PNG = 'data:image/png;base64,iVBORw0KGgo=';

function setup(has: boolean, sources: PreviewSource[] = []) {
  const files = {
    hasPreview: vi.fn((_id: string, _current?: boolean) => of(has)),
    putPreview: vi.fn((_id: string, _v: number, _png: Blob, _sources?: readonly PreviewSource[]) => of(undefined as void)),
  };
  const draw = vi.fn(async () => PNG as string | null);
  return { files, draw, preview: new FilePreview(files, draw, () => sources) };
}

const file = (version: number) => ({ id: 'f1', name: 'flow.drawio', content_version: version }) as FileAttachment;

describe('FilePreview', () => {
  it('draws a shown diagram that has no preview of its version, once', async () => {
    const { files, draw, preview } = setup(false);
    await preview.shown(file(3));
    await preview.shown(file(3));
    expect(files.hasPreview).toHaveBeenCalledTimes(1);
    expect(draw).toHaveBeenCalledTimes(1);
    expect(files.putPreview).toHaveBeenCalledWith('f1', 3, expect.any(Blob), []);
    expect(files.hasPreview).toHaveBeenCalledWith('f1', false);
    expect((files.putPreview.mock.calls[0][2] as Blob).type).toBe('image/png');
  });

  it('leaves one that has a preview alone', async () => {
    const { files, draw, preview } = setup(true);
    await preview.shown(file(3));
    expect(draw).not.toHaveBeenCalled();
    expect(files.putPreview).not.toHaveBeenCalled();
  });

  it('draws every saved version', async () => {
    const { files, preview } = setup(true);
    await preview.saved(file(4));
    expect(files.hasPreview).not.toHaveBeenCalled();
    expect(files.putPreview).toHaveBeenCalledWith('f1', 4, expect.any(Blob), []);
  });

  it('tries again next time when sending failed', async () => {
    const { files, preview } = setup(false);
    files.putPreview.mockReturnValueOnce(throwError(() => new Error('409')));
    await preview.shown(file(3));
    await preview.shown(file(3));
    expect(files.putPreview).toHaveBeenCalledTimes(2);
  });

  it('sends a page’s picture with the files it loaded, and draws it again once one of them changed', async () => {
    const sources: PreviewSource[] = [{ id: 'css', content_version: 2 }];
    const { files, draw, preview } = setup(false, sources);
    await preview.shown(file(3));
    expect(files.hasPreview).toHaveBeenCalledWith('f1', true);
    expect(files.putPreview).toHaveBeenCalledWith('f1', 3, expect.any(Blob), [{ id: 'css', content_version: 2 }]);
    await preview.shown(file(3));
    expect(draw).toHaveBeenCalledTimes(1);
    // The page reloaded with the stylesheet an agent changed: asked again, and drawn again.
    sources[0] = { id: 'css', content_version: 3 };
    await preview.shown(file(3));
    expect(files.hasPreview).toHaveBeenCalledTimes(2);
    expect(files.putPreview).toHaveBeenLastCalledWith('f1', 3, expect.any(Blob), [{ id: 'css', content_version: 3 }]);
  });
});

import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { describe, expect, it, vi } from 'vitest';

import { DIAGRAMS_URL } from './diagram-site.config';
import { DiagramUploads, drawioNameFor, titleFor } from './diagram-uploads';

function uploads(site: string | null): DiagramUploads {
  TestBed.resetTestingModule();
  TestBed.configureTestingModule({ providers: [{ provide: DIAGRAMS_URL, useValue: site }] });
  return TestBed.inject(DiagramUploads);
}

describe('DiagramUploads', () => {
  it('passes every other file through as it is, at once', () => {
    const file = new File(['a'], 'notes.md');
    let got: File | undefined;
    uploads('https://diagrams.example.test/index.html')
      .prepare(file)
      .subscribe(f => (got = f));
    expect(got).toBe(file);
  });

  it('uploads a Visio drawing as the .drawio it converts to', async () => {
    const service = uploads('https://diagrams.example.test/index.html');
    const spy = vi.fn().mockResolvedValue('<mxfile/>');
    service.convertVisio = spy;
    const file = new File(['PK'], 'Org chart.vsdx');
    const out = await firstValueFrom(service.prepare(file));
    expect(spy).toHaveBeenCalledWith(expect.anything(), 'https://diagrams.example.test/index.html', file, 'Org chart.vsdx');
    expect(out.name).toBe('Org chart.drawio');
    expect(out.type).toBe('application/vnd.jgraph.mxfile');
    expect(await out.text()).toBe('<mxfile/>');
  });

  it('refuses Visio where there is no diagram editor, and the older binary formats', async () => {
    await expect(firstValueFrom(uploads(null).prepare(new File(['PK'], 'a.vsdx')))).rejects.toThrow('diagram editor');
    await expect(firstValueFrom(uploads('https://d.example.test/').prepare(new File(['x'], 'a.vsd')))).rejects.toThrow('Save it as .vsdx');
  });
});

describe('Visio names', () => {
  it('names the .drawio after the drawing, and the title with it', () => {
    expect(drawioNameFor('flow.VSDX')).toBe('flow.drawio');
    const drawio = new File([''], 'flow.drawio');
    expect(titleFor('My flow.vsdx', drawio)).toBe('My flow.drawio');
    expect(titleFor('My flow', drawio)).toBe('My flow');
    expect(titleFor(undefined, drawio)).toBeUndefined();
  });
});

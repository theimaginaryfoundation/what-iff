import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { DiagramEditorComponent } from './diagram-editor.component';
import { DIAGRAMS_URL } from './diagram-site.config';

const SITE = 'https://diagrams.example.test/index.html';
const ORIGIN = 'https://diagrams.example.test';
const XML = '<mxfile><diagram id="p1"><mxGraphModel><root><mxCell id="0"/></root></mxGraphModel></diagram></mxfile>';

function file(): FileAttachment {
  return { id: 'f-1', name: 'flow.drawio', file_type: 'application/vnd.jgraph.mxfile' } as FileAttachment;
}

describe('DiagramEditorComponent', () => {
  let fixture: ComponentFixture<DiagramEditorComponent>;
  let posted: { message: Record<string, unknown>; origin: string }[];
  let frameWindow: Window;

  function setUp(site: string | null) {
    TestBed.configureTestingModule({
      imports: [DiagramEditorComponent],
      providers: [provideZonelessChangeDetection(), { provide: DIAGRAMS_URL, useValue: site }],
    });
    fixture = TestBed.createComponent(DiagramEditorComponent);
  }

  async function open(readOnly: boolean, text = XML) {
    fixture.componentRef.setInput('file', file());
    fixture.componentRef.setInput('blob', new Blob([text]));
    fixture.componentRef.setInput('readOnly', readOnly);
    fixture.detectChanges();
    await fixture.whenStable();
    const iframe = fixture.nativeElement.querySelector('iframe') as HTMLIFrameElement;
    posted = [];
    frameWindow = {
      postMessage: vi.fn((m: string, origin: string) => posted.push({ message: JSON.parse(m), origin })),
    } as unknown as Window;
    Object.defineProperty(iframe, 'contentWindow', { value: frameWindow });
    return iframe;
  }

  /** A message from the editor frame. */
  async function fromEditor(message: Record<string, unknown>, origin = ORIGIN, source: Window = frameWindow) {
    window.dispatchEvent(new MessageEvent('message', { data: JSON.stringify(message), origin, source }));
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
  }

  beforeEach(() => setUp(SITE));

  it('loads the file into the editor once it is ready, read-only while viewing', async () => {
    const iframe = await open(true);
    expect(iframe.getAttribute('src')).toContain('chrome=0');
    expect(fixture.nativeElement.textContent).toContain('Opening the diagram');
    await fromEditor({ event: 'init' });
    expect(posted).toEqual([{ message: { action: 'load', xml: XML, autosave: 1, title: 'flow.drawio' }, origin: ORIGIN }]);
    await fromEditor({ event: 'load', xml: XML });
    expect(fixture.nativeElement.textContent).not.toContain('Opening the diagram');
    // A viewer takes no changes.
    await fromEditor({ event: 'autosave', xml: '<mxfile>changed</mxfile>' });
    expect(fixture.componentInstance.dirty()).toBe(false);
  });

  it('tracks changes while editing, hands out the latest XML, and is clean once saved', async () => {
    const iframe = await open(false);
    expect(iframe.getAttribute('src')).not.toContain('chrome=0');
    const dirty: boolean[] = [];
    fixture.componentInstance.dirtyChange.subscribe(d => dirty.push(d));
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({ event: 'autosave', xml: '<mxfile>v2</mxfile>' });
    expect(fixture.componentInstance.dirty()).toBe(true);
    const saved = await fixture.componentInstance.content();
    expect(await saved.text()).toBe('<mxfile>v2</mxfile>');
    expect(saved.type).toBe('application/vnd.jgraph.mxfile');
    fixture.componentInstance.markSaved(saved);
    expect(fixture.componentInstance.dirty()).toBe(false);
    expect(dirty).toEqual([true, false]);

    // The workspace hands back the saved bytes: nothing is loaded again.
    posted.length = 0;
    fixture.componentRef.setInput('blob', saved);
    fixture.detectChanges();
    await fixture.whenStable();
    expect(posted).toEqual([]);
  });

  it('ignores messages from anywhere but the editor frame', async () => {
    await open(false);
    await fromEditor({ event: 'init' }, 'https://evil.example.test');
    await fromEditor({ event: 'init' }, ORIGIN, {} as Window);
    expect(posted).toEqual([]);
  });

  it('asks the workspace to save on the editor’s save shortcut', async () => {
    await open(false);
    const save = vi.fn();
    fixture.componentInstance.saveRequested.subscribe(save);
    await fromEditor({ event: 'init' });
    await fromEditor({ event: 'load', xml: XML });
    await fromEditor({ event: 'save', xml: '<mxfile>v3</mxfile>' });
    expect(save).toHaveBeenCalledTimes(1);
    expect(fixture.componentInstance.dirty()).toBe(true);
  });

  it('says so when this server has no diagram editor', async () => {
    TestBed.resetTestingModule();
    setUp(null);
    fixture.componentRef.setInput('file', file());
    fixture.componentRef.setInput('blob', new Blob([XML]));
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('iframe')).toBeNull();
    expect(fixture.nativeElement.textContent).toContain('no diagram editor set up');
  });
});

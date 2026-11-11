import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { FILEROBOT_LOADER, ImageEditorComponent } from './image-editor.component';

interface FakeEditorInstance {
  container: HTMLElement;
  config: Record<string, unknown>;
  renderConfig: Record<string, unknown> | undefined;
  terminated: boolean;
}

function fakeFilerobot() {
  const instances: FakeEditorInstance[] = [];
  const state = { failRender: false };
  class FakeFilerobot {
    // Statics, as on the real class: its typings put them on the instance, but they are not there.
    static TABS = { ADJUST: 'Adjust', FINETUNE: 'Finetune', FILTERS: 'Filters', ANNOTATE: 'Annotate', RESIZE: 'Resize' };
    static TOOLS = { CROP: 'Crop', ROTATE: 'Rotate' };
    readonly record: FakeEditorInstance;
    constructor(container: HTMLElement, config: Record<string, unknown>) {
      this.record = { container, config, renderConfig: undefined, terminated: false };
      instances.push(this.record);
    }
    render(additional?: Record<string, unknown>) {
      if (state.failRender) throw new Error('cannot render');
      this.record.renderConfig = additional;
    }
    terminate() {
      this.record.terminated = true;
    }
    getCurrentImgData(info: { extension: string }) {
      return { imageData: { imageBase64: `data:image/${info.extension};base64,aGk=`, mimeType: `image/${info.extension}` }, designState: {}, hideLoadingSpinner: () => undefined };
    }
  }
  return { ctor: FakeFilerobot, instances, state };
}

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u', name: 'photo.png', file_type: 'image/png', created_at: '2026-11-01T00:00:00Z', ...overrides };
}

describe('ImageEditorComponent', () => {
  let fixture: ComponentFixture<ImageEditorComponent>;
  let fake: ReturnType<typeof fakeFilerobot>;

  beforeEach(async () => {
    fake = fakeFilerobot();
    globalThis.URL.createObjectURL ??= () => '';
    globalThis.URL.revokeObjectURL ??= () => undefined;
    vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:image');
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ arrayBuffer: () => Promise.resolve(new TextEncoder().encode('hi').buffer) }));
    await TestBed.configureTestingModule({
      imports: [ImageEditorComponent],
      providers: [provideZonelessChangeDetection(), { provide: FILEROBOT_LOADER, useValue: () => Promise.resolve(fake.ctor) }],
    }).compileComponents();
    fixture = TestBed.createComponent(ImageEditorComponent);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  async function open(value: FileAttachment): Promise<void> {
    fixture.componentRef.setInput('file', value);
    fixture.componentRef.setInput('blob', new Blob(['png'], { type: value.file_type }));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).toBe('ready'));
    fixture.detectChanges();
  }

  it('mounts the editor on the image bytes with the workspace owning the save', async () => {
    await open(file());
    expect(fake.instances).toHaveLength(1);
    const { config, renderConfig } = fake.instances[0];
    expect(config['source']).toBe('blob:image');
    expect(config['useBackendTranslations']).toBe(false);
    expect(config['showBackButton']).toBe(false);
    expect(renderConfig?.['tabsIds']).toEqual(['Adjust', 'Finetune', 'Filters', 'Annotate', 'Resize']);
    // Opening on Crop saves small images cropped (see the comment in create()).
    expect(renderConfig?.['defaultToolId']).toBe('Rotate');
    expect(fixture.componentInstance.unsupported()).toBe(false);
  });

  it('reports changes, hands the edited image back in the file format, and marks saved', async () => {
    const dirty: boolean[] = [];
    fixture.componentInstance.dirtyChange.subscribe(d => dirty.push(d));
    await open(file({ file_type: 'image/jpeg', name: 'photo.jpg' }));
    (fake.instances[0].config['onModify'] as () => void)();
    expect(fixture.componentInstance.dirty()).toBe(true);
    expect(dirty).toEqual([true]);
    const blob = await fixture.componentInstance.content();
    expect(blob.type).toBe('image/jpeg');
    expect(await blob.text()).toBe('hi');
    expect(fetch).toHaveBeenCalledWith('data:image/jpeg;base64,aGk=');
    fixture.componentInstance.markSaved();
    expect(fixture.componentInstance.dirty()).toBe(false);
  });

  it('refuses to write formats it cannot produce, and says so', async () => {
    await open(file({ file_type: 'image/gif', name: 'anim.gif' }));
    expect(fixture.componentInstance.unsupported()).toBe(true);
    expect((fixture.nativeElement as HTMLElement).textContent).toContain('cannot write back');
    await expect(fixture.componentInstance.content()).rejects.toThrow('cannot write image/gif');
  });

  it('tears the editor down and releases the object URL', async () => {
    await open(file());
    fixture.destroy();
    expect(fake.instances[0].terminated).toBe(true);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:image');
  });

  it('stays dirty for a change made during a save and does not reopen its own bytes', async () => {
    await open(file());
    const modify = fake.instances[0].config['onModify'] as () => void;
    modify();
    const saved = await fixture.componentInstance.content();
    modify();
    fixture.componentRef.setInput('blob', saved);
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.componentInstance.markSaved(saved);
    expect(fake.instances.length).toBe(1);
    expect(fixture.componentInstance.dirty()).toBe(true);
  });

  it('shows an error, not a spinner, when the editor cannot start', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined);
    fake.state.failRender = true;
    fixture.componentRef.setInput('file', file());
    fixture.componentRef.setInput('blob', new Blob(['png'], { type: 'image/png' }));
    fixture.detectChanges();
    await vi.waitFor(() => expect(fixture.componentInstance.state()).toBe('error'));
    expect(URL.revokeObjectURL).toHaveBeenCalled();
  });
});

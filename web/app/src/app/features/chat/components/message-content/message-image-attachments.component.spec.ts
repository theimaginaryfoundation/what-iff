import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { FileAttachment } from '../../../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../../../core/services/file-attachment.service';
import { DiagramPictures } from '../../../diagrams/diagram-pictures.service';
import { FileWorkspaceService } from '../../../file-workspace/file-workspace.service';
import { MessageImageAttachmentsComponent } from './message-image-attachments.component';

const diagram = { id: 'd-1', name: 'flow.drawio', content_version: 2 } as FileAttachment;

function setUp() {
  const pictures = { picture: vi.fn(() => of('data:image/png;base64,iVBORw==')) };
  const files = { downloadFileAttachment: vi.fn(() => of(new Blob(['<mxfile/>']))) };
  const workspace = { requestOpen: vi.fn() };
  TestBed.configureTestingModule({
    imports: [MessageImageAttachmentsComponent],
    providers: [
      provideZonelessChangeDetection(),
      provideHttpClient(),
      provideHttpClientTesting(),
      { provide: DiagramPictures, useValue: pictures },
      { provide: FileAttachmentService, useValue: files },
      { provide: FileWorkspaceService, useValue: workspace },
    ],
  });
  const fixture = TestBed.createComponent(MessageImageAttachmentsComponent);
  fixture.componentRef.setInput('attachments', [diagram]);
  fixture.componentRef.setInput('kind', 'diagram');
  fixture.detectChanges();
  return { fixture, pictures, files, workspace };
}

describe('MessageImageAttachmentsComponent with diagrams', () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows a diagram's picture and opens it in the workspace", async () => {
    const { fixture, pictures, workspace } = setUp();
    await fixture.whenStable();
    const img = fixture.nativeElement.querySelector('img.message-images__diagram') as HTMLImageElement;
    expect(img.getAttribute('src')).toBe('data:image/png;base64,iVBORw==');
    expect(img.alt).toBe('flow.drawio');
    expect(pictures.picture).toHaveBeenCalledWith(diagram);
    (fixture.nativeElement.querySelector('.message-images__preview') as HTMLButtonElement).click();
    expect(workspace.requestOpen).toHaveBeenCalledWith('d-1');
  });

  it('downloads the diagram file itself, not its picture', async () => {
    const { fixture, files } = setUp();
    let saved: { name: string; blob: Blob } | undefined;
    let blob: Blob | undefined;
    vi.spyOn(URL, 'createObjectURL').mockImplementation(b => ((blob = b as Blob), 'blob:x'));
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => undefined);
    vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      saved = { name: this.download, blob: blob! };
    });
    (fixture.nativeElement.querySelector('.message-images__download') as HTMLButtonElement).click();
    expect(files.downloadFileAttachment).toHaveBeenCalledWith('d-1');
    expect(saved?.name).toBe('flow.drawio');
    expect(await saved?.blob.text()).toBe('<mxfile/>');
  });
});

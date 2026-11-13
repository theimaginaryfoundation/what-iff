import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { of } from 'rxjs';
import { describe, expect, it, vi } from 'vitest';

import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { ContextPanelService } from '../chat/services/context-panel.service';
import { DIAGRAMS_URL } from './diagram-site.config';
import { MermaidDiagramOpener, mermaidFileName } from './mermaid-to-drawio';

describe('mermaidFileName', () => {
  it('names the file after the diagram’s title, or its kind', () => {
    expect(mermaidFileName('---\ntitle: Sign-up flow\n---\nflowchart LR\n A --> B')).toBe('Sign-up flow.drawio');
    expect(mermaidFileName('%% a note\nsequenceDiagram\n A->>B: hi')).toBe('sequenceDiagram.drawio');
    expect(mermaidFileName('')).toBe('diagram.drawio');
  });
});

describe('MermaidDiagramOpener', () => {
  function setup(url: string, site: string | null = 'https://diagrams.example.test/index.html') {
    const files = { uploadChatFileAttachment: vi.fn((_chat: string, f: File) => of({ id: 'chat-file', name: f.name })) };
    const gallery = { importImage: vi.fn((f: File) => of({ id: 'gallery-file', name: f.name })) };
    const requestComposerAttachment = vi.fn();
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        { provide: DIAGRAMS_URL, useValue: site },
        { provide: FileAttachmentService, useValue: files },
        { provide: ImageGalleryService, useValue: gallery },
        { provide: ContextPanelService, useValue: { requestComposerAttachment } },
      ],
    });
    const router = TestBed.inject(Router);
    vi.spyOn(router, 'url', 'get').mockReturnValue(url);
    const navigate = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    const opener = TestBed.inject(MermaidDiagramOpener);
    opener.convert = vi.fn().mockResolvedValue('<mxfile/>');
    return { opener, files, gallery, requestComposerAttachment, navigate };
  }

  it('in a conversation, saves the diagram there, opens it in its workspace and puts it on the message being written', async () => {
    const { opener, files, gallery, requestComposerAttachment, navigate } = setup('/chat/c1?file=x');
    await opener.open('flowchart LR\n A --> B');
    expect(files.uploadChatFileAttachment).toHaveBeenCalledWith('c1', expect.any(File));
    expect((files.uploadChatFileAttachment.mock.calls[0][1] as File).name).toBe('flowchart.drawio');
    expect(gallery.importImage).not.toHaveBeenCalled();
    expect(requestComposerAttachment).toHaveBeenCalledWith(expect.objectContaining({ id: 'chat-file' }));
    expect(navigate).toHaveBeenCalledWith(['/chat', 'c1'], { queryParams: { file: 'chat-file' } });
  });

  it('does nothing where there is no diagram editor', async () => {
    const { opener, files, gallery, navigate } = setup('/chat/c1', null);
    await opener.open('flowchart LR\n A --> B');
    expect(opener.convert).not.toHaveBeenCalled();
    expect(files.uploadChatFileAttachment).not.toHaveBeenCalled();
    expect(gallery.importImage).not.toHaveBeenCalled();
    expect(navigate).not.toHaveBeenCalled();
  });

  it('anywhere else, saves it in the gallery and opens it there', async () => {
    const { opener, files, gallery, navigate } = setup('/gallery?file=doc');
    await opener.open('pie\n "a": 1');
    expect(gallery.importImage).toHaveBeenCalled();
    expect(files.uploadChatFileAttachment).not.toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith(['/gallery'], { queryParams: { file: 'gallery-file' } });
  });
});

import { HttpClient } from '@angular/common/http';
import { ChangeDetectionStrategy, Component, ElementRef, HostListener, OnDestroy, OnInit, inject, signal, viewChild } from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { FileAttachment } from '../core/models/file-attachment.model';
import { FileAttachmentService } from '../core/services/file-attachment.service';
import { ImageGalleryService } from '../core/services/image-gallery.service';
import { environment } from '../../environments/environment';
import { loadMonaco } from './monaco-spike.component';

type Monaco = typeof import('monaco-editor');

/** The Monaco language whose registered extensions include the file's, else plain text. */
export function monacoLanguageFor(monaco: Monaco, fileName: string): string {
  const dot = fileName.lastIndexOf('.');
  const ext = dot >= 0 ? fileName.slice(dot).toLowerCase() : '';
  if (!ext) return 'plaintext';
  const hit = monaco.languages.getLanguages().find(l => l.extensions?.some(e => e.toLowerCase() === ext));
  return hit?.id ?? 'plaintext';
}

/**
 * Throwaway spike for ADR 0x043 step 0: Monaco as the editor for a file in the gallery (the
 * virtual file system). Metadata and bytes come through the existing services; Save goes to the
 * step 1 endpoint `PUT /file-attachment/{id}/content` with the content version as If-Match.
 * Route: /spike/vfs?file=<attachment id>
 */
@Component({
  selector: 'app-vfs-editor-spike',
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div style="display:flex; flex-direction:column; height:100vh; font: 13px system-ui">
      <div style="display:flex; gap:12px; align-items:center; padding:8px 12px; border-bottom:1px solid #888">
        <strong data-testid="name">{{ file()?.folder ? file()!.folder + '/' : '' }}{{ file()?.name ?? '…' }}</strong>
        <span data-testid="language">{{ language() }}</span>
        <span data-testid="state">{{ state() }}</span>
        <button data-testid="save" (click)="save()" [disabled]="!dirty()">Save (Ctrl+S)</button>
        <span data-testid="version">v{{ version() }}</span>
      </div>
      <div #host data-testid="editor" style="flex:1; min-height:0"></div>
    </div>
  `,
})
export class VfsEditorSpikeComponent implements OnInit, OnDestroy {
  private readonly route = inject(ActivatedRoute);
  private readonly http = inject(HttpClient);
  private readonly files = inject(FileAttachmentService);
  private readonly gallery = inject(ImageGalleryService);
  private readonly host = viewChild.required<ElementRef<HTMLElement>>('host');

  readonly file = signal<FileAttachment | null>(null);
  readonly language = signal('');
  readonly state = signal('loading');
  readonly dirty = signal(false);
  /** The content version the editor loaded; step 1 adds it to FileAttachment. Until then, 1. */
  readonly version = signal(1);
  private editor?: import('monaco-editor').editor.IStandaloneCodeEditor;
  private model?: import('monaco-editor').editor.ITextModel;
  private monaco?: Monaco;

  async ngOnInit(): Promise<void> {
    const id = this.route.snapshot.queryParamMap.get('file');
    if (!id) { this.state.set('no ?file='); return; }
    const [meta, blob, monaco] = await Promise.all([
      firstValueFrom(this.gallery.getFileInfo(id)),
      firstValueFrom(this.files.downloadFileAttachment(id)),
      loadMonaco(),
    ]);
    this.monaco = monaco;
    this.file.set(meta);
    this.version.set((meta as FileAttachment & { content_version?: number }).content_version ?? 1);
    const text = await blob.text();
    const lang = monacoLanguageFor(monaco, meta.name);
    this.language.set(lang);
    // A stable URI per VFS path, so the JSON/TS services and "go to" features key on it.
    const uri = monaco.Uri.parse(`vfs:///${meta.folder ? meta.folder + '/' : ''}${meta.name}`);
    this.model = monaco.editor.getModel(uri) ?? monaco.editor.createModel(text, lang, uri);
    (globalThis as { __spikeModel?: unknown }).__spikeModel = this.model; // test hook
    this.editor = monaco.editor.create(this.host().nativeElement, { model: this.model, automaticLayout: true });
    this.model.onDidChangeContent(() => { this.dirty.set(true); this.state.set('modified'); });
    this.editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => void this.save());
    this.state.set('ready');
  }

  @HostListener('window:keydown', ['$event'])
  onKeydown(e: KeyboardEvent): void {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); void this.save(); }
  }

  async save(): Promise<void> {
    const f = this.file(); const model = this.model;
    if (!f || !model || !this.dirty()) return;
    this.state.set('saving');
    try {
      const res = await firstValueFrom(this.http.put<{ content_version: number }>(
        `${environment.apiUrl}/file-attachment/${f.id}/content`, model.getValue(),
        { headers: { 'Content-Type': f.file_type || 'text/plain', 'If-Match': String(this.version()) } }));
      this.version.set(res.content_version);
      this.dirty.set(false);
      this.state.set('saved');
    } catch (err) {
      const status = (err as { status?: number }).status;
      this.state.set(status === 412 ? 'conflict: the file changed, reload or save as' : `save failed (${status})`);
    }
  }

  ngOnDestroy(): void { this.editor?.dispose(); this.model?.dispose(); }
}

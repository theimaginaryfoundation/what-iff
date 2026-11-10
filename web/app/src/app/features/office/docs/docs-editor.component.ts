import { ChangeDetectionStrategy, Component, inject, signal } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { DocDocument } from '../docx/univer-doc-types';
import { DOCX_MIME, documentToDocx } from '../docx/docx-export';
import { docxToDocument } from '../docx/docx-import';
import { FidelityReport } from '../shared/fidelity';
import { FidelityNoticeComponent } from '../shared/fidelity-notice.component';
import { UniverEditorBase, UniverKit, UniverUnit } from '../univer/univer-editor-base';
import { FUniver, UNIVER_DOCS_LOADER, UniverDocsKit } from '../univer/univer-loader';

const GUIDE_ATTEMPTS = 40;
const GUIDE_RETRY_MS = 100;

interface Document extends UniverUnit<DocDocument> {
  getPermission?(): { setEditable(value?: boolean): Promise<void> | void };
}

/**
 * The document editor (ADR 0x043, office suite): Univer Docs over the document our docx
 * converter reads. Saving hands back a .docx written over the original package (patch mode), so
 * every part the editor does not model is kept as it was.
 */
@Component({
  selector: 'app-docs-editor',
  standalone: true,
  imports: [FidelityNoticeComponent],
  templateUrl: '../univer/univer-editor.component.html',
  styleUrl: '../univer/univer-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DocsEditorComponent extends UniverEditorBase<DocDocument, Document> {
  private readonly load = inject(UNIVER_DOCS_LOADER);
  readonly noun = 'document';
  /** The bytes the open document came from; the exporter patches them rather than writing from scratch. */
  private original: ArrayBuffer | null = null;

  /** The margin corners and the lines of borderless table cells; off, the page looks as it prints. */
  readonly showGuides = signal(false);
  private docsKit: UniverDocsKit | null = null;
  private unitId: string | null = null;
  private guideTimers: ReturnType<typeof setTimeout>[] = [];

  protected async loadKit(): Promise<UniverKit> {
    const kit = await this.load(this.document);
    this.docsKit = kit;
    return kit;
  }

  toggleGuides(): void {
    this.showGuides.update(show => !show);
    this.applyGuides();
  }

  /** Univer renders the page shortly after the unit exists, so this tries until it can. */
  private applyGuides(attempt = 0): void {
    if (!this.univer || !this.unitId) return;
    const applied = this.docsKit?.setDocGuides?.(this.univer, this.unitId, this.showGuides() && !this.readOnly());
    if (!applied && attempt < GUIDE_ATTEMPTS) this.guideTimers.push(setTimeout(() => this.applyGuides(attempt + 1), GUIDE_RETRY_MS));
  }

  protected override onDispose(): void {
    this.guideTimers.forEach(clearTimeout);
    this.guideTimers = [];
    this.unitId = null;
  }

  protected async parse(blob: Blob, file: FileAttachment): Promise<{ data: DocDocument; report: FidelityReport }> {
    const bytes = await blob.arrayBuffer();
    this.original = null;
    const result = await docxToDocument(bytes, { id: `doc-${file.id}`, name: file.name });
    this.original = bytes;
    return { data: result.document, report: result.report };
  }

  protected async createUnit(univerAPI: FUniver, data: DocDocument, readOnly: boolean): Promise<Document> {
    const api = univerAPI as FUniver & { createDocument(data: unknown): Document };
    // A disabled snapshot is read-only without the permission layer's "content is protected"
    // toasts on every click; the viewer stays quiet.
    const document = api.createDocument(readOnly ? { ...data, disabled: true } : data);
    this.unitId = data.id;
    this.applyGuides();
    return document;
  }

  protected async serialize(data: DocDocument): Promise<Blob> {
    const { bytes, report } = await documentToDocx(data, this.original ?? undefined);
    this.exportReport.set(report);
    return new Blob([bytes as BlobPart], { type: DOCX_MIME });
  }
}

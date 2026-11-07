import { ChangeDetectionStrategy, Component, inject } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { DocDocument } from '../docx/univer-doc-types';
import { DOCX_MIME, documentToDocx } from '../docx/docx-export';
import { docxToDocument } from '../docx/docx-import';
import { FidelityReport } from '../shared/fidelity';
import { FidelityNoticeComponent } from '../shared/fidelity-notice.component';
import { UniverEditorBase, UniverKit, UniverUnit } from '../univer/univer-editor-base';
import { FUniver, UNIVER_DOCS_LOADER } from '../univer/univer-loader';

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

  protected loadKit(): Promise<UniverKit> {
    return this.load(this.document);
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
    return document;
  }

  protected async serialize(data: DocDocument): Promise<Blob> {
    const { bytes } = await documentToDocx(data, this.original ?? undefined);
    return new Blob([bytes as BlobPart], { type: DOCX_MIME });
  }
}

import { ChangeDetectionStrategy, Component, computed, inject } from '@angular/core';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { csvDelimiterFor, previewKindFor } from '../../gallery/helpers/file-preview.helpers';
import { FidelityReport } from '../shared/fidelity';
import { UniverEditorBase, UniverKit, UniverUnit } from '../univer/univer-editor-base';
import { FUniver, UNIVER_SHEETS_LOADER } from '../univer/univer-loader';
import { UniverWorkbook } from '../xlsx/univer-sheet-types';
import { sheetToCsv, workbookToXlsx } from '../xlsx/xlsx-export';
import { delimitedTextToWorkbook, xlsxToWorkbook } from '../xlsx/xlsx-import';

export { MUTATION } from '../univer/univer-editor-base';

export const XLSX_MIME = 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet';

interface Workbook extends UniverUnit<UniverWorkbook> {
  setEditable(value: boolean): unknown;
}

/**
 * The spreadsheet editor (ADR 0x043, office suite): Univer Sheets over the workbook our xlsx
 * converter reads. Saving hands back an .xlsx, or CSV for a CSV file.
 */
@Component({
  selector: 'app-sheets-editor',
  standalone: true,
  templateUrl: '../univer/univer-editor.component.html',
  styleUrl: '../univer/univer-editor.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SheetsEditorComponent extends UniverEditorBase<UniverWorkbook, Workbook> {
  private readonly load = inject(UNIVER_SHEETS_LOADER);
  readonly noun = 'spreadsheet';
  readonly isCsv = computed(() => previewKindFor(this.file()) === 'csv');

  protected loadKit(): Promise<UniverKit> {
    return this.load(this.document);
  }

  protected async parse(blob: Blob, file: FileAttachment): Promise<{ data: UniverWorkbook; report: FidelityReport }> {
    const id = `wb-${file.id}`;
    const result =
      previewKindFor(file) === 'csv'
        ? delimitedTextToWorkbook(await blob.text(), csvDelimiterFor(file), { id, name: file.name })
        : await xlsxToWorkbook(await blob.arrayBuffer(), { id, name: file.name });
    return { data: result.workbook, report: result.report };
  }

  protected createUnit(univerAPI: FUniver, data: UniverWorkbook, readOnly: boolean): Workbook {
    const api = univerAPI as FUniver & { createWorkbook(data: unknown): Workbook };
    const workbook = api.createWorkbook(data);
    workbook.setEditable(!readOnly);
    return workbook;
  }

  protected async serialize(data: UniverWorkbook, file: FileAttachment): Promise<Blob> {
    if (previewKindFor(file) === 'csv') {
      const first = data.sheets[data.sheetOrder[0]];
      return new Blob([sheetToCsv(first, csvDelimiterFor(file))], { type: file.file_type || 'text/csv' });
    }
    const { bytes } = await workbookToXlsx(data);
    return new Blob([bytes as BlobPart], { type: XLSX_MIME });
  }
}

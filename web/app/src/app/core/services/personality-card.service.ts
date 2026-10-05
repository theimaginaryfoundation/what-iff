import { Injectable, inject } from '@angular/core';
import { Observable, catchError, concatMap, from, map, of, switchMap, toArray } from 'rxjs';

import { Personality, PersonalityCardImportResult, buildPersonalityUpdateRequest } from '../models/personality.model';
import { filenameFromContentDisposition, saveBlobAsFile } from '../utils/download.helpers';
import { FileAttachmentService } from './file-attachment.service';
import { PersonalityService } from './personality.service';

/** What a character-card import produced, once the personality exists and its lore is attached. */
export interface PersonalityCardImportOutcome {
  personality: Personality;
  /** Non-fatal notes from the server (disabled entries skipped, fields dropped to fit the prompt limit, ...). */
  warnings: string[];
  /** For a PNG card: the picture could not be attached as the cover image (the personality was still created). */
  coverFailed: boolean;
  /** Number of character-book entries attached to the personality as files. */
  attachedLoreFiles: number;
  /** Names of character-book entries whose upload failed; the personality itself was still created. */
  failedLoreFiles: string[];
}

function isPngFile(file: File): boolean {
  return file.type === 'image/png' || file.name.toLowerCase().endsWith('.png');
}

/**
 * SillyTavern character-card import/export for personalities. The conversion rules live on the
 * server (`internal/stcard`); this service only moves files: it reads the chosen card (JSON, or a
 * PNG whose picture becomes the cover image), posts it, uploads any character-book entries the
 * server could not fold into the prompt, and saves exports.
 */
@Injectable({ providedIn: 'root' })
export class PersonalityCardService {
  private readonly personalityService = inject(PersonalityService);
  private readonly fileAttachmentService = inject(FileAttachmentService);

  /** Imports a JSON or PNG card file. Errors from reading or creating the personality surface on the stream. */
  importCardFile(file: File): Observable<PersonalityCardImportOutcome> {
    const png = isPngFile(file);
    const body$: Observable<string | ArrayBuffer> = png ? from(file.arrayBuffer()) : from(file.text());
    return body$.pipe(
      switchMap(body => this.personalityService.importSillyTavernCard(body, png ? 'image/png' : 'application/json')),
      switchMap(result => (png ? this.attachCover(result, file) : of({ result, coverFailed: false }))),
      switchMap(({ result, coverFailed }) => this.attachLoreFiles(result, coverFailed)),
    );
  }

  /**
   * Downloads the personality as a SillyTavern card and saves it to the user's machine: JSON, or
   * (`png`) the cover image with the card embedded in it.
   */
  exportCard(personality: Pick<Personality, 'id' | 'name'>, format: 'json' | 'png' = 'json'): Observable<void> {
    return this.personalityService.exportSillyTavernCard(personality.id, format).pipe(
      map(response => {
        if (!response.body) return;
        const filename = filenameFromContentDisposition(
          response.headers.get('Content-Disposition'),
          `${personality.name || 'personality'}.${format}`,
        );
        saveBlobAsFile(response.body, filename);
      }),
    );
  }

  /** Uploads the card's picture and makes it the personality's cover. Failure is recorded, not fatal. */
  private attachCover(
    result: PersonalityCardImportResult,
    picture: File,
  ): Observable<{ result: PersonalityCardImportResult; coverFailed: boolean }> {
    const { personality } = result;
    return this.fileAttachmentService.uploadPersonalityFileAttachment(personality.id, picture).pipe(
      switchMap(attachment =>
        this.personalityService.updatePersonality(
          personality.id,
          buildPersonalityUpdateRequest(personality, { cover_image_id: attachment.id }),
        ),
      ),
      map(updated => ({ result: { ...result, personality: updated }, coverFailed: false })),
      catchError(() => of({ result, coverFailed: true })),
    );
  }

  /** Uploads each lore file in turn (a failed one is recorded, not fatal) and summarizes the import. */
  private attachLoreFiles(result: PersonalityCardImportResult, coverFailed: boolean): Observable<PersonalityCardImportOutcome> {
    const { personality, warnings, lore_files: loreFiles } = result;
    if (loreFiles.length === 0) {
      return of({ personality, warnings, coverFailed, attachedLoreFiles: 0, failedLoreFiles: [] });
    }

    return from(loreFiles).pipe(
      concatMap(entry => {
        const file = new File([entry.content], entry.file_name, { type: 'text/markdown' });
        const description = entry.keys.length > 0 ? `Keywords: ${entry.keys.join(', ')}` : undefined;
        return this.fileAttachmentService
          .uploadPersonalityFileAttachment(personality.id, file, { description })
          .pipe(
            map((): string | null => null),
            catchError(() => of(entry.name)),
          );
      }),
      toArray(),
      map(failures => {
        const failedLoreFiles = failures.filter((name): name is string => name !== null);
        return {
          personality,
          warnings,
          coverFailed,
          attachedLoreFiles: loreFiles.length - failedLoreFiles.length,
          failedLoreFiles,
        };
      }),
    );
  }
}

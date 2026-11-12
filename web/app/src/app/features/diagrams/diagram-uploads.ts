import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';
import { Observable, defer, of } from 'rxjs';

import { DIAGRAMS_URL } from './diagram-site.config';
import { convertVisioToDrawio } from './drawio-convert';

/** The media type of a .drawio file. */
export const DRAWIO_MIME_TYPE = 'application/vnd.jgraph.mxfile';

/** Whether a file is a Visio drawing (.vsdx), which is uploaded as a .drawio diagram. */
export function isVisioFile(name: string): boolean {
  return /\.vsdx$/i.test(name);
}

/** Visio's older binary formats, which draw.io converts only with a server. */
function isLegacyVisioFile(name: string): boolean {
  return /\.(vsd|vss|vst)$/i.test(name);
}

/** The .drawio name of a converted Visio file. */
export function drawioNameFor(visioName: string): string {
  return `${visioName.replace(/\.vsdx$/i, '') || 'diagram'}.drawio`;
}

/** A title given with an upload, for the file it became: a Visio title names the .drawio instead. */
export function titleFor(title: string | undefined, prepared: File): string | undefined {
  return title && isVisioFile(title) && !isVisioFile(prepared.name) ? drawioNameFor(title) : title;
}

/**
 * Uploads of diagrams (ADR 0x049 §7): a Visio drawing is converted to a .drawio diagram in the
 * browser before it is uploaded, so it is stored, edited and read by agents as every diagram is;
 * the Visio file itself is never stored. Every other file is passed through as it is, at once.
 */
@Injectable({ providedIn: 'root' })
export class DiagramUploads {
  private readonly document = inject(DOCUMENT);
  private readonly site = inject(DIAGRAMS_URL);
  /** Converts a Visio drawing; replaced in tests. */
  convertVisio = convertVisioToDrawio;

  /** The file to upload in place of `file`: a .drawio for a Visio drawing, otherwise `file` itself. */
  prepare(file: File): Observable<File> {
    if (isLegacyVisioFile(file.name)) {
      return defer(() => Promise.reject(new Error(`"${file.name}" is an older Visio format. Save it as .vsdx in Visio, then add it.`)));
    }
    if (!isVisioFile(file.name)) return of(file);
    const site = this.site;
    if (!site) {
      return defer(() =>
        Promise.reject(
          new Error(`"${file.name}" can't be added: Visio files are converted in the diagram editor, which this server doesn't have.`),
        ),
      );
    }
    return defer(async () => {
      const xml = await this.convertVisio(this.document, site, file, file.name);
      return new File([xml], drawioNameFor(file.name), { type: DRAWIO_MIME_TYPE });
    });
  }
}

import { InjectionToken } from '@angular/core';

import type { PptxParser } from './pptx-import';
import type { PptxGenFactory } from './pptx-export';

/** pptxtojson and pptxgenjs are loaded on demand; tests provide fakes (or the real libraries) here. */
export const PPTX_PARSER = new InjectionToken<() => Promise<PptxParser>>('PPTX_PARSER', {
  providedIn: 'root',
  factory: () => () => import('pptxtojson').then(m => m.parse as PptxParser),
});

export const PPTX_WRITER = new InjectionToken<PptxGenFactory>('PPTX_WRITER', {
  providedIn: 'root',
  factory: () => async () => new (await import('pptxgenjs')).default(),
});

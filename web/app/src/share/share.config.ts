import { ApplicationConfig, SecurityContext, provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideRouter, withComponentInputBinding } from '@angular/router';
import { MARKED_EXTENSIONS, SANITIZE, provideMarkdown } from 'ngx-markdown';

import { latexMathExtension } from '../app/core/utils/latex-math.helpers';
import { mermaidExtension } from '../app/core/utils/mermaid.helpers';
import { readOnlyViewerProviders } from './share.providers';
import { shareRoutes } from './share.routes';

/** The share site's app: no sign-in, no interceptors, the app's viewers read-only. */
export const shareConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    provideRouter(shareRoutes, withComponentInputBinding()),
    provideHttpClient(withXhr()),
    provideMarkdown({
      sanitize: { provide: SANITIZE, useValue: SecurityContext.HTML },
      markedExtensions: [
        { provide: MARKED_EXTENSIONS, useFactory: latexMathExtension, multi: true },
        { provide: MARKED_EXTENSIONS, useFactory: mermaidExtension, multi: true },
      ],
    }),
    ...readOnlyViewerProviders(),
  ],
};

import { APP_INITIALIZER, ApplicationConfig, SecurityContext, provideZonelessChangeDetection } from '@angular/core';
import { PreloadAllModules, provideRouter, withPreloading } from '@angular/router';
import { provideHttpClient, withInterceptors, withXhr } from '@angular/common/http';
import { MARKED_EXTENSIONS, SANITIZE, provideMarkdown } from 'ngx-markdown';

import { routes } from './app.routes';
import { latexMathExtension } from './core/utils/latex-math.helpers';
import { authInterceptor } from './core/interceptors/auth.interceptor';
import { extraHttpInterceptors } from './extensions/http-interceptors';
import { ExternalAuthProvider } from './core/auth/external-auth.provider';
import { externalAuthProviders } from './extensions/external-auth.providers';
import { chatSendGateProviders } from './extensions/chat-send-gate.providers';
import { accessGateProviders } from './extensions/access-gate.providers';
import { integrationTabProviders } from './extensions/integration-tabs.providers';
import { threadAutomationProviders } from './extensions/thread-automations.providers';

export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    // Fetch the lazy route chunks in the background after boot, so the first tap on a nav item
    // does not wait on a chunk download before anything happens (#348).
    provideRouter(routes, withPreloading(PreloadAllModules)),
    provideHttpClient(withXhr(), withInterceptors([authInterceptor, ...extraHttpInterceptors])),
    provideMarkdown({
      sanitize: { provide: SANITIZE, useValue: SecurityContext.HTML },
      markedExtensions: [{ provide: MARKED_EXTENSIONS, useFactory: latexMathExtension, multi: true }],
    }),
    ...externalAuthProviders,
    ...chatSendGateProviders,
    ...accessGateProviders,
    ...integrationTabProviders,
    ...threadAutomationProviders,
    {
      provide: APP_INITIALIZER,
      multi: true,
      deps: [ExternalAuthProvider],
      useFactory: (auth: ExternalAuthProvider) => () => auth.configure(),
    },
  ]
};

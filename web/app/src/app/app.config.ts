import { APP_INITIALIZER, ApplicationConfig, SecurityContext, provideZonelessChangeDetection } from '@angular/core';
import { provideRouter } from '@angular/router';
import { provideHttpClient, withInterceptors, withXhr } from '@angular/common/http';
import { SANITIZE, provideMarkdown } from 'ngx-markdown';

import { routes } from './app.routes';
import { authInterceptor } from './core/interceptors/auth.interceptor';
import { ExternalAuthProvider } from './core/auth/external-auth.provider';
import { externalAuthProviders } from './extensions/external-auth.providers';
import { chatSendGateProviders } from './extensions/chat-send-gate.providers';
import { accessGateProviders } from './extensions/access-gate.providers';

export const appConfig: ApplicationConfig = {
  providers: [
    provideZonelessChangeDetection(),
    provideRouter(routes),
    provideHttpClient(withXhr(), withInterceptors([authInterceptor])),
    provideMarkdown({
      sanitize: { provide: SANITIZE, useValue: SecurityContext.HTML },
    }),
    ...externalAuthProviders,
    ...chatSendGateProviders,
    ...accessGateProviders,
    {
      provide: APP_INITIALIZER,
      multi: true,
      deps: [ExternalAuthProvider],
      useFactory: (auth: ExternalAuthProvider) => () => auth.configure(),
    },
  ]
};

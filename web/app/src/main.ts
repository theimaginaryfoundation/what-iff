import { bootstrapApplication } from '@angular/platform-browser';
import { appConfig } from './app/app.config';
import { App } from './app/app';
import { ThemeService } from './app/core/services/theme.service';

ThemeService.initializeThemeEarly();

bootstrapApplication(App, appConfig)
  .catch((err) => console.error(err));

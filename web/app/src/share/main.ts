import { bootstrapApplication } from '@angular/platform-browser';

import { ThemeService } from '../app/core/services/theme.service';
import { ShareAppComponent } from './share-app.component';
import { shareConfig } from './share.config';

// The share site (ADR 0x051 §5): a read-only viewer of what users share, with nothing of the app's account.
ThemeService.initializeThemeEarly();

bootstrapApplication(ShareAppComponent, shareConfig).catch(err => console.error(err));

import { EmojiSearch } from '@ctrl/ngx-emoji-mart';
import { bootstrapApplication } from '@angular/platform-browser';
import { appConfig } from './app/app.config';
import { App } from './app/app';
import { ThemeService } from './app/core/services/theme.service';
import { installComposerEmojiShortcodes } from './app/features/chat/helpers/emoji-shortcode.helpers';

ThemeService.initializeThemeEarly();

bootstrapApplication(App, appConfig)
  .then(appRef => installComposerEmojiShortcodes(appRef.injector.get(EmojiSearch)))
  .catch((err) => console.error(err));

/**
 * The share site's settings (ADR 0x051 §5). In development the web apps site is the app's dev
 * server's /preview/ on 127.0.0.1 (another origin than the share site's localhost:4204); a
 * deployment's build replaces this file with share-environment.docker.ts.
 */
export const shareEnvironment = {
  previewUrl: 'http://127.0.0.1:4200/preview/index.html',
};

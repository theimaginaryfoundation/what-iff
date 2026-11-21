/**
 * The app's settings as the share site builds the app's viewers with them (ADR 0x051 §5), in place
 * of src/environments/environment.ts: no API, no sign-in, no other site. The share site reaches
 * only its own /api/shared (share-api.service.ts), and its web apps site comes from
 * share-environment.ts; anything that reached for the app's would find nothing to reach.
 */
export const environment = {
  production: true,
  apiUrl: '',
  authMode: 'local' as const,
  passwordPolicy: {
    minLength: 8,
    requireLowercase: true,
    requireUppercase: true,
    requireNumber: true,
    requireSymbol: true,
  },
  enableGalleryExpressionAssignment: false,
  docsUrl: '',
  diagramsUrl: '',
  previewUrl: '',
  shareUrl: '',
  jupyterliteUrl: '',
};

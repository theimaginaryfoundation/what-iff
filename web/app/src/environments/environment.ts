export const environment = {
  production: false,
  apiUrl: 'http://localhost:8080/api',
  authMode: 'local' as const,
  passwordPolicy: {
    minLength: 8,
    requireLowercase: true,
    requireUppercase: true,
    requireNumber: true,
    requireSymbol: true
  },
  // Gallery feature flag for expression assignment action.
  enableGalleryExpressionAssignment: true,
  docsUrl: 'https://whatiff.chat/docs.html'
};

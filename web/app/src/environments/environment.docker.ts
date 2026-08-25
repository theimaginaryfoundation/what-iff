export const environment = {
  production: false,
  apiUrl: '__API_URL__',
  authMode: '__AUTH_MODE__' as const,
  passwordPolicy: {
    minLength: 8,
    requireLowercase: true,
    requireUppercase: true,
    requireNumber: true,
    requireSymbol: true
  },
  // Gallery feature flag for expression assignment action. Substituted like
  // the other deployment values; defaults to false when the build arg is unset.
  enableGalleryExpressionAssignment: ['true'].includes('__ENABLE_GALLERY_EXPRESSION_ASSIGNMENT__'),
  docsUrl: '__DOCS_URL__'
};

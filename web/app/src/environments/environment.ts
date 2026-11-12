export const environment = {
  production: false,
  apiUrl: 'http://localhost:8080/api',
  authMode: 'local' as const,
  passwordPolicy: {
    minLength: 8,
    requireLowercase: true,
    requireUppercase: true,
    requireNumber: true,
    requireSymbol: true,
  },
  // Gallery feature flag for expression assignment action.
  enableGalleryExpressionAssignment: true,
  docsUrl: 'https://whatiff.chat/docs.html',
  // The draw.io editor for .drawio files, on an origin of its own: make diagrams-site diagrams-serve.
  diagramsUrl: 'http://localhost:4204/index.html',
  // The JupyterLite site that runs notebooks, on an origin of its own: make jupyterlite-serve.
  jupyterliteUrl: 'http://localhost:4203/lab/index.html',
};

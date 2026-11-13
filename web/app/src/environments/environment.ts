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
  // The draw.io editor for .drawio files, on an origin of its own (ADR 0x049): `make diagrams-site`, then
  // ng serve serves it at /diagrams/; 127.0.0.1 is another origin than localhost. Open the app on localhost.
  diagramsUrl: 'http://127.0.0.1:4200/diagrams/index.html',
  // The JupyterLite site that runs notebooks, on an origin of its own: make jupyterlite-serve.
  jupyterliteUrl: 'http://localhost:4203/lab/index.html',
};

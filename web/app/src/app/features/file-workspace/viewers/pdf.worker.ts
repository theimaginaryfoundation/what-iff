// The PDF.js worker as its own bundle (ADR 0x043 step 0): the application builder emits it as a
// separate file next to the app, so it loads from the app's own origin in the mobile shell too.
import 'pdfjs-dist/build/pdf.worker.min.mjs';

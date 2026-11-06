# ADR 0x043 step 0 spike scripts (throwaway)

Run from this directory with `web/app/node_modules` symlinked as `./node_modules` (they import
playwright, exceljs, pdf-lib, mammoth, docx and pdfjs-dist from it) and a sample PDF as `sample.pdf`:

- `node monaco-check.mjs <path to a production build's browser dir>` after `ng build --configuration production` on this branch (route `/spike/monaco`).
- `node docx-make.mjs && esbuild docx-entry.js --bundle --format=esm --minify --outfile=docx-entry.bundle.js && node docx-check.mjs`
- `node xlsx-make.mjs && esbuild univer-entry.js --bundle --format=esm --minify --outfile=univer-entry.bundle.js --loader:.woff2=file --loader:.woff=file --loader:.ttf=file --loader:.svg=file --loader:.png=file && node univer-check.mjs`
- `node pdf-check.mjs '<json of extra annotationStorage entries>'`

Results are recorded in `docs/adr/0x043-file-workspace.md`, "Step 0 results".

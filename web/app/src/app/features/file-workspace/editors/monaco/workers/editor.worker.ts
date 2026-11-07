// Monaco's editor worker as its own bundle (ADR 0x043 step 0): the application builder emits it next
// to the app, so it loads from the app's own origin in the mobile shell too.
import 'monaco-editor/editor/editor.worker';

import { JupyterFrontEnd, JupyterFrontEndPlugin } from '@jupyterlab/application';
import { PageConfig } from '@jupyterlab/coreutils';
import { IDocumentManager } from '@jupyterlab/docmanager';

/**
 * The bridge between WhatIff and the JupyterLite embedded in it (ADR 0x043 §8). The host sends the
 * notebook; it goes in the default drive, so a kernel can read it and its neighbours with plain
 * `open()`, and opens. Saves go back to the host, which persists them through its usual save.
 *
 *   host -> here   whatiff:open { name, content }, whatiff:close
 *   here -> host   whatiff:ready, whatiff:save { content }, whatiff:closed
 *
 * The drive is in memory (see jupyter-lite.json), so one notebook's files are not there for the next
 * notebook or the next account on this browser.
 *
 * This site holds nothing of the user's beyond the notebook it was handed, and it has no credentials:
 * it lives on an origin of its own, and only origins in `whatiffHostOrigins` may talk to it.
 */
const plugin: JupyterFrontEndPlugin<void> = {
  id: 'whatiff-bridge:plugin',
  autoStart: true,
  requires: [IDocumentManager],
  activate: (app: JupyterFrontEnd, documents: IDocumentManager) => {
    if (window.parent === window) return;
    const allowed = (PageConfig.getOption('whatiffHostOrigins') || '')
      .split(',')
      .map(origin => origin.trim())
      .filter(Boolean);
    const contents = app.serviceManager.contents;
    let hostOrigin: string | null = null;
    let path: string | null = null;
    /** The latest save being sent to the host; closing waits for it. */
    let posting: Promise<void> = Promise.resolve();

    const post = (origin: string, notebookPath: string): Promise<void> =>
      contents.get(notebookPath, { content: true }).then(model => {
        window.parent.postMessage({ type: 'whatiff:save', content: model.content }, origin);
      });

    // Notebook code can write this origin's storage on purpose, and the next account to use this
    // browser would run on the same origin. Nothing of ours lives there (the drive is in memory),
    // so it is cleared when the runner starts and before each notebook opens.
    const clearStorage = async (): Promise<void> => {
      try {
        localStorage.clear();
        sessionStorage.clear();
      } catch {
        // Storage can be unavailable; then there is nothing to clear.
      }
      const databases = (await indexedDB.databases?.().catch(() => [])) ?? [];
      for (const db of databases) {
        // JupyterLite's own store is open and in memory-only use; deleting it would only block.
        if (db.name && !db.name.startsWith('JupyterLite Storage')) indexedDB.deleteDatabase(db.name);
      }
    };
    void clearStorage();

    const open = async (name: string, content: unknown): Promise<void> => {
      await app.restored;
      await clearStorage();
      const target = name.replace(/[/\\]/g, '_') || 'notebook.ipynb';
      // Set after the write, so writing the host's own copy is not reported back as a save.
      await contents.save(target, { type: 'notebook', format: 'json', content });
      path = target;
      documents.openOrReveal(target, 'Notebook');
    };

    /** Saves a notebook still being edited, so going back to the host loses nothing. */
    const close = async (): Promise<void> => {
      const widget = path ? documents.findWidget(path) : undefined;
      const context = widget ? documents.contextForWidget(widget) : undefined;
      if (context?.model.dirty) {
        await context.save();
        // Sent here, not only from fileChanged, so it reaches the host before whatiff:closed.
        if (path && hostOrigin) posting = post(hostOrigin, path);
      }
      await posting;
    };

    contents.fileChanged.connect((_, change) => {
      if (change.type !== 'save' || !path || change.newValue?.path !== path || !hostOrigin) return;
      posting = post(hostOrigin, path).catch(error => console.error('whatiff-bridge: could not send the save', error));
    });

    window.addEventListener('message', event => {
      if (event.source !== window.parent || !allowed.includes(event.origin)) return;
      const message = event.data as { type?: string; name?: string; content?: unknown };
      hostOrigin = event.origin;
      if (message?.type === 'whatiff:open' && typeof message.name === 'string') {
        void open(message.name, message.content).catch(error => console.error('whatiff-bridge: could not open the notebook', error));
      } else if (message?.type === 'whatiff:close') {
        const origin = event.origin;
        void close()
          .catch(error => console.error('whatiff-bridge: could not save before closing', error))
          .finally(() => window.parent.postMessage({ type: 'whatiff:closed' }, origin));
      }
    });

    // The parent is checked when it answers; this message carries nothing.
    void app.restored.then(() => window.parent.postMessage({ type: 'whatiff:ready' }, '*'));
  },
};

export default plugin;

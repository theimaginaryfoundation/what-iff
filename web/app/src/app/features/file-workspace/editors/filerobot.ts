import { createElement } from 'react';
import { type Root, createRoot } from 'react-dom/client';
import FilerobotImageEditor, {
  type FilerobotImageEditorConfig,
  type getCurrentImgDataFunction,
  TABS,
  TOOLS,
} from 'react-filerobot-image-editor';

/**
 * Mounts the Filerobot image editor (MIT, a React component) into a plain DOM element, using the
 * app's own React.
 *
 * This replaces Filerobot's vanilla wrapper, `filerobot-image-editor`. That package bundles React 18
 * and its own react-dom, but the editor component it renders is a peer dependency that resolves the
 * app's React 19. The two Reacts in one tree fail on the first hook ("Cannot read properties of
 * null (reading 'useEffect')"). Mounting the component here keeps one React, one react-konva and
 * one styled-components for the whole editor.
 *
 * Only the image editor imports this, lazily, so none of it is in the app's main bundle.
 */
export class FilerobotEditor {
  static readonly TABS = TABS;
  static readonly TOOLS = TOOLS;

  private readonly root: Root;
  private readonly imgDataRef: { current?: getCurrentImgDataFunction } = {};
  private readonly config: FilerobotImageEditorConfig;

  constructor(container: HTMLElement, config: FilerobotImageEditorConfig) {
    this.root = createRoot(container);
    this.config = config;
  }

  /** Renders the editor with the constructor's config, `extra`'s top-level keys replacing its own. */
  render(extra: Partial<FilerobotImageEditorConfig> = {}): void {
    const config = { ...this.config, ...extra, getCurrentImgDataFnRef: this.imgDataRef };
    this.root.render(createElement(FilerobotImageEditor, config));
  }

  /** The edited image; empty until the editor has rendered. */
  getCurrentImgData(...args: Parameters<getCurrentImgDataFunction>): Partial<ReturnType<getCurrentImgDataFunction>> {
    return this.imgDataRef.current?.(...args) ?? {};
  }

  terminate(): void {
    this.root.unmount();
  }
}

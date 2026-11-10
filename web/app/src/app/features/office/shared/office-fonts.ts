import { DOCUMENT } from '@angular/common';
import { DestroyRef, inject } from '@angular/core';

/** The aliases for Office font names, built as a lazy global style (angular.json `styles`). */
export const OFFICE_FONTS_STYLESHEET = 'office-fonts.css';

let users = 0;
let link: HTMLLinkElement | null = null;
let ready: Promise<void> = Promise.resolve();

/**
 * Links the Office font aliases while something needs them. They redefine names such as Consolas
 * and Segoe UI that the app's own font stacks also use, so they must not stay on the page once
 * the last office editor closes. Resolves when the aliases can be used, including if they failed
 * to load, when a document simply falls back to other fonts.
 */
export function acquireOfficeFonts(document: Document): { ready: Promise<void>; release: () => void } {
  if (users++ === 0) {
    const element = document.createElement('link');
    element.rel = 'stylesheet';
    element.href = OFFICE_FONTS_STYLESHEET;
    link = element;
    // Without the font loading API there is no text rendering to hold back for.
    ready = document.fonts
      ? new Promise(resolve => {
          element.onload = element.onerror = () => resolve();
        })
      : Promise.resolve();
    document.head.appendChild(element);
  }
  let released = false;
  return {
    ready,
    release: () => {
      if (released) return;
      released = true;
      if (--users === 0) {
        link?.remove();
        link = null;
      }
    },
  };
}

/** The aliases for as long as the calling component lives. */
export function useOfficeFonts(): Promise<void> {
  const fonts = acquireOfficeFonts(inject(DOCUMENT));
  inject(DestroyRef).onDestroy(fonts.release);
  return fonts.ready;
}

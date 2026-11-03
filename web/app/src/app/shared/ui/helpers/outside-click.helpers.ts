import { DOCUMENT } from '@angular/common';
import { DestroyRef, ElementRef, inject } from '@angular/core';

/**
 * Calls `onOutside` for every click whose target is outside the current component's host.
 * Must be called in an injection context (a constructor or field initializer).
 *
 * The listener runs in the capture phase on the document, so a `stopPropagation()` between the
 * target and the document cannot hide the click. A bubbling `document:click` misses those: the
 * chat composer stops clicks on its textarea and controls row (so they don't refocus the
 * textarea), which left its popovers open when you clicked there (#398).
 */
export function listenForOutsideClick(onOutside: (event: MouseEvent) => void): void {
  const host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
  const document = inject(DOCUMENT);
  const listener = (event: MouseEvent) => {
    const target = event.target;
    if (target instanceof Node && !host.contains(target)) onOutside(event);
  };
  document.addEventListener('click', listener, true);
  inject(DestroyRef).onDestroy(() => document.removeEventListener('click', listener, true));
}

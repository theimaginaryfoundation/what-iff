import { isTabKey } from './keyboard.helpers';

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

export interface FocusTrapHandle {
  readonly host: HTMLElement;
  readonly restoreTo: HTMLElement | null;
  readonly onKeydown: (event: KeyboardEvent) => void;
  release: () => void;
}

export function createFocusTrap(
  host: HTMLElement,
  opts: { restoreTo?: HTMLElement | null } = {},
): FocusTrapHandle {
  const restoreTo = opts.restoreTo ?? (host.ownerDocument.activeElement as HTMLElement | null);

  const focusFirst = (): void => {
    const first = getFocusableElements(host)[0] ?? host;
    first.focus();
  };

  const onKeydown = (event: KeyboardEvent): void => {
    if (!isTabKey(event)) {
      return;
    }

    const focusable = getFocusableElements(host);

    if (focusable.length === 0) {
      event.preventDefault();
      host.focus();
      return;
    }

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    const active = host.ownerDocument.activeElement;

    if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  };

  host.addEventListener('keydown', onKeydown);

  if (!host.hasAttribute('tabindex')) {
    host.setAttribute('tabindex', '-1');
  }

  focusFirst();

  return {
    host,
    restoreTo,
    onKeydown,
    release: () => {
      host.removeEventListener('keydown', onKeydown);
      restoreTo?.focus();
    },
  };
}

export function releaseFocusTrap(handle: FocusTrapHandle | null | undefined): void {
  handle?.release();
}

export function getFocusableElements(host: HTMLElement): HTMLElement[] {
  return Array.from(host.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
    .filter(element => !element.hasAttribute('disabled') && element.tabIndex !== -1);
}

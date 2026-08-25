import { createFocusTrap, getFocusableElements, releaseFocusTrap } from './focus-trap.helpers';

describe('focus trap helpers', () => {
    let host: HTMLElement;
    let before: HTMLButtonElement;
    let first: HTMLButtonElement;
    let last: HTMLButtonElement;
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        before = document.createElement('button');
        before.textContent = 'Before';
        host = document.createElement('div');
        first = document.createElement('button');
        first.textContent = 'First';
        last = document.createElement('button');
        last.textContent = 'Last';

        host.append(first, last);
        container.append(before, host);
        document.body.append(container);
        before.focus();
    });

    afterEach(() => {
        container.remove();
    });

    it('finds focusable descendants', () => {
        expect(getFocusableElements(host)).toEqual([first, last]);
    });

    it('focuses the first focusable element when created', () => {
        const handle = createFocusTrap(host);

        expect(document.activeElement).toBe(first);

        releaseFocusTrap(handle);
    });

    it('wraps tab focus within the host', () => {
        const handle = createFocusTrap(host);
        last.focus();

        const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true });
        last.dispatchEvent(event);

        expect(event.defaultPrevented).toBe(true);
        expect(document.activeElement).toBe(first);

        releaseFocusTrap(handle);
    });

    it('restores focus on release', () => {
        const handle = createFocusTrap(host);

        releaseFocusTrap(handle);

        expect(document.activeElement).toBe(before);
    });
});

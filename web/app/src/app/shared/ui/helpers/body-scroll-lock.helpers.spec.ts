import { lockBodyScroll, releaseBodyScroll, resetBodyScrollLockForTests, } from './body-scroll-lock.helpers';

describe('body scroll lock helpers', () => {
    let body: HTMLElement;

    beforeEach(() => {
        resetBodyScrollLockForTests();
        body = document.createElement('body');
    });

    it('adds and removes the overflow-hidden class', () => {
        const handle = lockBodyScroll(body);

        expect(body.classList.contains('overflow-hidden')).toBe(true);

        releaseBodyScroll(handle);

        expect(body.classList.contains('overflow-hidden')).toBe(false);
    });

    it('preserves a pre-existing overflow-hidden class', () => {
        body.classList.add('overflow-hidden');
        const handle = lockBodyScroll(body);

        releaseBodyScroll(handle);

        expect(body.classList.contains('overflow-hidden')).toBe(true);
    });

    it('keeps the body locked until all locks are released', () => {
        const first = lockBodyScroll(body);
        const second = lockBodyScroll(body);

        releaseBodyScroll(first);
        expect(body.classList.contains('overflow-hidden')).toBe(true);

        releaseBodyScroll(second);
        expect(body.classList.contains('overflow-hidden')).toBe(false);
    });
});

import { isActivationKey, isEscapeKey, isTabKey } from './keyboard.helpers';

function keyEvent(key: string, init: KeyboardEventInit = {}): KeyboardEvent {
    return new KeyboardEvent('keydown', { key, ...init });
}

describe('keyboard helpers', () => {
    it('recognizes unmodified Enter and Space as activation keys', () => {
        expect(isActivationKey(keyEvent('Enter'))).toBe(true);
        expect(isActivationKey(keyEvent(' '))).toBe(true);
    });

    it('ignores modified activation keys', () => {
        expect(isActivationKey(keyEvent('Enter', { ctrlKey: true }))).toBe(false);
        expect(isActivationKey(keyEvent(' ', { shiftKey: true }))).toBe(false);
    });

    it('recognizes escape keys', () => {
        expect(isEscapeKey(keyEvent('Escape'))).toBe(true);
        expect(isEscapeKey(keyEvent('Esc'))).toBe(true);
        expect(isEscapeKey(keyEvent('Enter'))).toBe(false);
    });

    it('recognizes tab keys', () => {
        expect(isTabKey(keyEvent('Tab'))).toBe(true);
        expect(isTabKey(keyEvent('ArrowRight'))).toBe(false);
    });
});

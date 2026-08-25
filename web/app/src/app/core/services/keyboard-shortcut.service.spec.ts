import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { KeyboardShortcutService } from './keyboard-shortcut.service';

describe('KeyboardShortcutService', () => {
    let service: KeyboardShortcutService;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [provideZonelessChangeDetection(), KeyboardShortcutService],
        });
        service = TestBed.inject(KeyboardShortcutService);
    });

    function dispatch(init: KeyboardEventInit, target: EventTarget = document): void {
        const event = new KeyboardEvent('keydown', { bubbles: true, ...init });
        target.dispatchEvent(event);
    }

    it('fires the handler for Cmd+K (metaKey)', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'k', metaOrCtrl: true }, handler);
        dispatch({ key: 'k', metaKey: true });
        expect(handler).toHaveBeenCalledTimes(1);
    });

    it('fires the handler for Ctrl+K (ctrlKey)', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'k', metaOrCtrl: true }, handler);
        dispatch({ key: 'k', ctrlKey: true });
        expect(handler).toHaveBeenCalledTimes(1);
    });

    it('does not fire for plain k', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'k', metaOrCtrl: true }, handler);
        dispatch({ key: 'k' });
        expect(handler).not.toHaveBeenCalled();
    });

    it('skips firing while focus is in an input by default', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'k', metaOrCtrl: true }, handler);
        const input = document.createElement('input');
        document.body.appendChild(input);
        try {
            dispatch({ key: 'k', metaKey: true }, input);
            expect(handler).not.toHaveBeenCalled();
        }
        finally {
            input.remove();
        }
    });

    it('fires inside inputs when allowInInputs is true', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'k', metaOrCtrl: true, allowInInputs: true }, handler);
        const input = document.createElement('input');
        document.body.appendChild(input);
        try {
            dispatch({ key: 'k', metaKey: true }, input);
            expect(handler).toHaveBeenCalledTimes(1);
        }
        finally {
            input.remove();
        }
    });

    it('respects shift / alt modifiers', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'k', metaOrCtrl: true, shift: true }, handler);

        dispatch({ key: 'k', metaKey: true });
        expect(handler).not.toHaveBeenCalled();

        dispatch({ key: 'k', metaKey: true, shiftKey: true });
        expect(handler).toHaveBeenCalledTimes(1);
    });

    it('returns a disposer that stops firing once called', () => {
        const handler = vi.fn().mockName('handler');
        const dispose = service.register({ key: 'k', metaOrCtrl: true }, handler);
        dispatch({ key: 'k', metaKey: true });
        dispose();
        dispatch({ key: 'k', metaKey: true });
        expect(handler).toHaveBeenCalledTimes(1);
    });

    it('matches keys case-insensitively', () => {
        const handler = vi.fn().mockName('handler');
        service.register({ key: 'K', metaOrCtrl: true }, handler);
        dispatch({ key: 'k', metaKey: true });
        expect(handler).toHaveBeenCalledTimes(1);
    });
});

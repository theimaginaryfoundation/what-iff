import { HttpErrorResponse } from '@angular/common/http';

import { defaultComposerPlaceholder, isHttpErrorResponse } from './chat-send.helpers';

describe('isHttpErrorResponse', () => {
    it('is true for an HttpErrorResponse', () => {
        expect(isHttpErrorResponse(new HttpErrorResponse({ status: 500 }))).toBe(true);
    });

    it('is false for a plain error or non-error value', () => {
        expect(isHttpErrorResponse(new Error('nope'))).toBe(false);
        expect(isHttpErrorResponse('nope')).toBe(false);
        expect(isHttpErrorResponse(null)).toBe(false);
    });
});

describe('defaultComposerPlaceholder', () => {
    it('includes the personality name when provided', () => {
        expect(defaultComposerPlaceholder('Ada')).toBe('Message Ada... (type / for skills)');
    });

    it('falls back to a generic placeholder when the name is blank', () => {
        expect(defaultComposerPlaceholder('   ')).toBe('Message... (type / for skills)');
        expect(defaultComposerPlaceholder(null)).toBe('Message... (type / for skills)');
    });
});

import { HttpErrorResponse } from '@angular/common/http';
import { apiErrorEnvelope, apiErrorMessage, hasErrorCode } from './api-error.helpers';

/** A failed request as Angular delivers it: parsed envelope on `.error`. */
function httpFailure(status: number, body: unknown): HttpErrorResponse {
    return new HttpErrorResponse({
        status,
        statusText: 'Bad Request',
        url: 'http://localhost:8080/api/threads',
        error: body,
    });
}

describe('api-error.helpers', () => {
    describe('the bug this replaces', () => {
        // The six local helpers deleted alongside this file all began with
        // `if (error instanceof Error && error.message) return error.message`.
        // HttpErrorResponse implements Error but does not extend it, so that guard
        // misses and the next one reads Angular's transport string instead.
        it('HttpErrorResponse is not an Error, and its message is a transport string', () => {
            const failure = httpFailure(400, { message: 'That message is too long.', code: 'message_too_long' });

            expect(failure instanceof Error).toBe(false);
            expect(failure.message).toContain('Http failure response');
            expect(failure.message).toContain('http://localhost:8080/api/threads');
        });

        it('never returns the transport string, even with no envelope to fall back on', () => {
            const networkDrop = new HttpErrorResponse({ status: 0, statusText: 'Unknown Error', error: null });

            expect(apiErrorMessage(networkDrop, 'Failed to load threads')).toBe('Failed to load threads');
        });
    });

    describe('apiErrorMessage', () => {
        it('prefers the envelope message', () => {
            const failure = httpFailure(400, { message: 'That message is too long.', code: 'message_too_long' });

            expect(apiErrorMessage(failure, 'Failed to send')).toBe('That message is too long.');
        });

        it('falls back to the deprecated error duplicate while it still ships', () => {
            const failure = httpFailure(403, { error: 'An active subscription is required.', code: 'forbidden' });

            expect(apiErrorMessage(failure, 'Failed to send')).toBe('An active subscription is required.');
        });

        it('ignores a blank or whitespace-only envelope message', () => {
            const failure = httpFailure(500, { message: '   ', error: '', code: 'internal_error' });

            expect(apiErrorMessage(failure, 'Something went wrong')).toBe('Something went wrong');
        });

        it('ignores a string body, which is a gateway page rather than an envelope', () => {
            const gateway = httpFailure(502, '<html><body>502 Bad Gateway</body></html>');

            expect(apiErrorMessage(gateway, 'Service unavailable')).toBe('Service unavailable');
        });

        // A DOM event body means the request never reached the server. ErrorEvent
        // carries a `message`, so without the Event check it would be mistaken for
        // an envelope and its browser string shown as though it were our copy.
        it('ignores a DOM event body and prefers the caller fallback', () => {
            const clientSide = httpFailure(0, new ErrorEvent('error', { message: 'Network request failed' }));

            expect(apiErrorMessage(clientSide, 'Could not reach the server')).toBe('Could not reach the server');
            expect(apiErrorEnvelope(clientSide)).toBeNull();
        });

        it('still reports genuine client-side throws', () => {
            expect(apiErrorMessage(new Error('Missing Cognito token after refresh'), 'Failed')).toBe('Missing Cognito token after refresh');
            expect(apiErrorMessage({ message: 'plain object throw' }, 'Failed')).toBe('plain object throw');
        });

        it('uses the fallback for values that carry no message at all', () => {
            expect(apiErrorMessage(undefined, 'Failed to load')).toBe('Failed to load');
            expect(apiErrorMessage('a bare string', 'Failed to load')).toBe('Failed to load');
        });
    });

    describe('apiErrorEnvelope', () => {
        it('returns null for anything that is not an HTTP failure with a parsed body', () => {
            expect(apiErrorEnvelope(new Error('boom'))).toBeNull();
            expect(apiErrorEnvelope(httpFailure(502, 'plain text'))).toBeNull();
            expect(apiErrorEnvelope(httpFailure(0, null))).toBeNull();
        });
    });

    describe('hasErrorCode', () => {
        it('matches a specific code', () => {
            const failure = httpFailure(402, { message: 'Out of credits.', code: 'quota_exceeded' });

            expect(hasErrorCode(failure, 'quota_exceeded')).toBe(true);
            expect(hasErrorCode(failure, 'message_too_long')).toBe(false);
        });

        it('is false when the request never produced an envelope', () => {
            expect(hasErrorCode(new Error('boom'), 'quota_exceeded')).toBe(false);
        });
    });
});

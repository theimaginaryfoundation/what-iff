import { HttpErrorResponse } from '@angular/common/http';
import { firstValueFrom } from 'rxjs';

import { apiErrorMessage } from '../utils/api-error.helpers';
import { decodeErrorBody } from './speech.service';

describe('decodeErrorBody', () => {
  it("reads an audio request's error body as the JSON envelope it is", async () => {
    const body = new TextEncoder().encode(JSON.stringify({ code: 'quota_exceeded', message: "You're out of credits." })).buffer;
    const err = new HttpErrorResponse({ status: 429, error: body });
    const decoded = await firstValueFrom(decodeErrorBody(err)).catch((e: unknown) => e);
    expect(decoded).toBeInstanceOf(HttpErrorResponse);
    expect((decoded as HttpErrorResponse).status).toBe(429);
    expect(apiErrorMessage(decoded, 'generic')).toBe("You're out of credits.");
  });

  it('keeps the status when the body is not JSON', async () => {
    const err = new HttpErrorResponse({ status: 502, error: new Uint8Array([1, 2]).buffer });
    const decoded = (await firstValueFrom(decodeErrorBody(err)).catch((e: unknown) => e)) as HttpErrorResponse;
    expect(decoded.status).toBe(502);
    expect(decoded.error).toBeNull();
  });
});

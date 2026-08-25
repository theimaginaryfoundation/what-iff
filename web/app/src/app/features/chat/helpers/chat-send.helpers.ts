import { HttpErrorResponse } from '@angular/common/http';

/** Narrows an unknown caught value to an Angular HttpErrorResponse. */
export function isHttpErrorResponse(error: unknown): error is HttpErrorResponse {
  return error instanceof HttpErrorResponse;
}

/** Composer placeholder shown when nothing constrains sending. */
export function defaultComposerPlaceholder(defaultName: string | null): string {
  const name = defaultName?.trim() || '';
  return name
    ? `Message ${name}... (type / for skills)`
    : 'Message... (type / for skills)';
}

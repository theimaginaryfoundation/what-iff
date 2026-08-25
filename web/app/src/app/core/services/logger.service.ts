import { Injectable } from '@angular/core';

@Injectable({
  providedIn: 'root'
})
export class LoggerService {
  error(message: string, error?: unknown): void {
    if (error instanceof Error) {
      // eslint-disable-next-line no-console
      console.error(message, error);
    } else if (error !== undefined) {
      // eslint-disable-next-line no-console
      console.error(message, error);
    } else {
      // eslint-disable-next-line no-console
      console.error(message);
    }
  }
}

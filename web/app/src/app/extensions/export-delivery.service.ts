import { Injectable } from '@angular/core';

export interface ExportDeliveryCopy {
  description: string;
  confirmation: string;
  completed: string;
}

/**
 * Build swap-point for account-export delivery copy.
 *
 * The open-source/local build stores exports on the API host and logs a file URL.
 * A private overlay replaces this service with copy for its configured email transport.
 */
@Injectable({ providedIn: 'root' })
export class ExportDeliveryService {
  readonly copy: ExportDeliveryCopy = {
    description:
      'We build a ZIP of your conversations, personalities, and memories and save it to local storage. When it is ready, find its file link in the API logs. Files are stored under LOCAL_FILE_STORE_DIR (default: /tmp/chat-app-files inside the API container).',
    confirmation:
      'Your account details, conversations, personalities, and memories will be included in the export.\n\nWe’ll save the ZIP to local storage. When it is ready, find its file link in the API logs. Files are stored under LOCAL_FILE_STORE_DIR (default: /tmp/chat-app-files inside the API container).\n\nPreparing your export may take some time.\n\nTo proceed, click “Confirm export” below.',
    completed: 'Your export is ready. Find its file link in the API logs.',
  };
}

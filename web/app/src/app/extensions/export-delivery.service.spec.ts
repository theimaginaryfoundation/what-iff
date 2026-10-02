import { TestBed } from '@angular/core/testing';

import { ExportDeliveryService } from './export-delivery.service';

describe('ExportDeliveryService', () => {
  it('explains how to locate a local export without promising email delivery', () => {
    const service = TestBed.inject(ExportDeliveryService);

    expect(service.copy.description).toContain('LOCAL_FILE_STORE_DIR');
    expect(service.copy.confirmation).toContain('API logs');
    expect(service.copy.completed).toContain('API logs');
    expect(service.copy.confirmation).not.toMatch(/email/i);
  });
});

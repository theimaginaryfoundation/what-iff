import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { environment } from '@environments/environment';
import { BackgroundNoticesService } from './background-notices.service';

describe('BackgroundNoticesService', () => {
  let service: BackgroundNoticesService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(BackgroundNoticesService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it("lists and dismisses a chat's notices", () => {
    service.list('chat-1').subscribe();
    http.expectOne({ method: 'GET', url: `${environment.apiUrl}/chat/chat-1/background-notices` }).flush([]);
    service.dismiss('chat-1', 'n1').subscribe();
    http.expectOne({ method: 'POST', url: `${environment.apiUrl}/chat/chat-1/background-notices/n1/dismiss` }).flush(null);
  });
});

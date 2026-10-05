import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';

import { environment } from '../../../environments/environment';
import { DiscordService } from './discord.service';

describe('DiscordService.available', () => {
  let service: DiscordService;
  let http: HttpTestingController;
  const url = `${environment.apiUrl}/discord/bindings`;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(DiscordService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('is on when the relay answers, and asks only once', async () => {
    const first = firstValueFrom(service.available());
    http.expectOne(url).flush([]);
    expect(await first).toBe(true);
    expect(await firstValueFrom(service.available())).toBe(true);
    http.expectNone(url);
  });

  it('is off when the server does not route /discord', async () => {
    const result = firstValueFrom(service.available());
    http.expectOne(url).flush({ message: 'not found' }, { status: 404, statusText: 'Not Found' });
    expect(await result).toBe(false);
  });

  it('stays on for other errors, so they surface where the user can see them', async () => {
    const result = firstValueFrom(service.available());
    http.expectOne(url).flush({ message: 'boom' }, { status: 500, statusText: 'Server Error' });
    expect(await result).toBe(true);
  });
});

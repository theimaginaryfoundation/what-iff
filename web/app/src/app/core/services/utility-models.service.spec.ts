import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { environment } from '@environments/environment';
import { UtilityModels } from '../models/utility-model.model';
import { UtilityModelsService } from './utility-models.service';

const BODY: UtilityModels = { accounts_choose: true, roles: [{ role: 'memory', account: null, deployment: null }] };

describe('UtilityModelsService', () => {
  let service: UtilityModelsService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(UtilityModelsService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('reads every role', () => {
    let got: UtilityModels | undefined;
    service.get().subscribe(b => (got = b));
    http.expectOne({ method: 'GET', url: `${environment.apiUrl}/utility-models` }).flush(BODY);
    expect(got).toEqual(BODY);
  });

  it("chooses a role's model with how it works, or switches the role off", () => {
    service.choose('simple', { model_id: 'model-1', reasoning_effort: 'low', service_tier: 'flex' }).subscribe();
    const put = http.expectOne({ method: 'PUT', url: `${environment.apiUrl}/utility-models/simple` });
    expect(put.request.body).toEqual({ model_id: 'model-1', reasoning_effort: 'low', service_tier: 'flex' });
    put.flush(BODY);

    service.switchOff('simple').subscribe();
    const off = http.expectOne({ method: 'PUT', url: `${environment.apiUrl}/utility-models/simple` });
    expect(off.request.body).toEqual({ off: true });
    off.flush(BODY);
  });
});

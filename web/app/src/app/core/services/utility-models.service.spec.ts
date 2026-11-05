import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { environment } from '@environments/environment';
import { UtilityModels } from '../models/utility-model.model';
import { UtilityModelsService } from './utility-models.service';

const BODY: UtilityModels = { roles: [{ role: 'memory', account_model_id: null, deployment_model_id: null, built_in_model: 'gpt-5.6-luna' }] };

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

  it("chooses and clears a role's model", () => {
    service.choose('quick', 'model-1').subscribe();
    const put = http.expectOne({ method: 'PUT', url: `${environment.apiUrl}/utility-models/quick` });
    expect(put.request.body).toEqual({ model_id: 'model-1' });
    put.flush(BODY);

    service.clear('quick').subscribe();
    http.expectOne({ method: 'DELETE', url: `${environment.apiUrl}/utility-models/quick` }).flush(BODY);
  });
});

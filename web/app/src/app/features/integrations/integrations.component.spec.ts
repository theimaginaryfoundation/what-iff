import { TestBed } from '@angular/core/testing';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { of } from 'rxjs';

import { environment } from '../../../environments/environment';
import { AccessGate } from '../../core/services/access-gate';
import { IntegrationsComponent } from './integrations.component';

describe('IntegrationsComponent built-in tools', () => {
  let http: HttpTestingController;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [IntegrationsComponent],
      providers: [
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: AccessGate, useValue: { hasAccess: () => of(false) } },
      ],
    }).compileComponents();

    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('renders human-facing tool descriptions from the tools API', () => {
    const fixture = TestBed.createComponent(IntegrationsComponent);
    fixture.detectChanges();

    http.expectOne(`${environment.apiUrl}/tools`).flush([
      { name: 'recall', description: 'Find relevant information from your saved memories and past conversations.' },
    ]);
    fixture.detectChanges();

    const page = fixture.nativeElement as HTMLElement;
    expect(page.textContent).toContain('Built-in tools');
    expect(page.textContent).toContain('recall');
    expect(page.textContent).toContain('Find relevant information from your saved memories and past conversations.');
  });

  it('keeps built-in tools visible without connector billing access', () => {
    const fixture = TestBed.createComponent(IntegrationsComponent);
    fixture.detectChanges();

    http.expectOne(`${environment.apiUrl}/tools`).flush([]);
    fixture.detectChanges();

    const page = fixture.nativeElement as HTMLElement;
    expect(page.querySelector('[data-testid="built-in-tools-list"]')).not.toBeNull();
    expect(page.textContent).not.toContain('Integrations are unavailable for this account');
  });
});

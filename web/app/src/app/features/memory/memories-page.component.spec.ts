import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { ActivatedRoute, Router } from '@angular/router';
import { of } from 'rxjs';

import { MemoriesPageComponent } from './memories-page.component';

describe('MemoriesPageComponent', () => {
  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [MemoriesPageComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: ActivatedRoute, useValue: { queryParams: of({}) } },
        { provide: Router, useValue: { navigate: vi.fn().mockName('navigate') } },
      ],
    }).compileComponents();
  });

  it('creates with memories tab active by default', () => {
    const fixture = TestBed.createComponent(MemoriesPageComponent);
    expect(fixture.componentInstance).toBeTruthy();
    expect(fixture.componentInstance.activeTab()).toBe('memories');
  });
});

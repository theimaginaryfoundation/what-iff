import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, throwError } from 'rxjs';

import { ProviderUsage } from '../../../core/models/provider-usage.model';
import { ProviderUsageService } from '../../../core/services/provider-usage.service';
import { ProvidersGuideComponent } from './providers-guide.component';

describe('ProvidersGuideComponent', () => {
  let fixture: ComponentFixture<ProvidersGuideComponent>;

  const usage: ProviderUsage[] = [
    { provider: 'anthropic', jobs: [{ job: 'Chatting with any Claude model', model: 'the model you pick' }] },
    {
      provider: 'openai',
      required: true,
      jobs: [{ job: 'Memory extraction and scratchpad updates', model: 'gpt-5.6-luna' }],
    },
  ];

  async function render(response: ProviderUsage[] | 'error'): Promise<void> {
    const service = {
      list: vi
        .fn()
        .mockName('list')
        .mockReturnValue(response === 'error' ? throwError(() => new Error('boom')) : of(response)),
    };

    await TestBed.configureTestingModule({
      imports: [ProvidersGuideComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        { provide: ProviderUsageService, useValue: service },
      ],
    }).compileComponents();

    fixture = TestBed.createComponent(ProvidersGuideComponent);
    fixture.detectChanges();
  }

  /**
   * The page's whole purpose is naming the model behind each job, so the model
   * column has to actually render — a job list without it is the vague answer
   * this page replaced.
   */
  it('names the model behind each job', async () => {
    await render(usage);

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Memory extraction and scratchpad updates');
    expect(text).toContain('gpt-5.6-luna');
  });

  it('puts the required provider first regardless of server order', async () => {
    await render(usage);

    const headings = Array.from(fixture.nativeElement.querySelectorAll('h2')) as HTMLElement[];
    expect(headings[0].textContent).toContain('OpenAI');
    expect(headings[0].textContent).toContain('Required');
  });

  it('reports a load failure instead of rendering an empty page', async () => {
    await render('error');

    expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain(
      'Could not load what each provider is used for.',
    );
  });
});

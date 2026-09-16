import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, throwError } from 'rxjs';

import { ProviderUsage } from '../../../core/models/provider-usage.model';
import { ProviderUsageService } from '../../../core/services/provider-usage.service';
import { ProvidersGuideComponent } from './providers-guide.component';

describe('ProvidersGuideComponent', () => {
  let fixture: ComponentFixture<ProvidersGuideComponent>;

  const providers: ProviderUsage[] = [
    { provider: 'anthropic', jobs: [{ job: 'Chatting with any Claude model', model: 'the model you pick' }] },
    {
      provider: 'openai',
      required: true,
      jobs: [{ job: 'Memory extraction and scratchpad updates', model: 'gpt-5.6-luna' }],
    },
  ];

  async function render(response: { accounts_supply_keys: boolean } | 'error'): Promise<void> {
    const service = {
      list: vi
        .fn()
        .mockName('list')
        .mockReturnValue(
          response === 'error'
            ? throwError(() => new Error('boom'))
            : of({ accounts_supply_keys: response.accounts_supply_keys, providers }),
        ),
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
    await render({ accounts_supply_keys: true });

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Memory extraction and scratchpad updates');
    expect(text).toContain('gpt-5.6-luna');
  });

  it('puts the required provider first regardless of server order', async () => {
    await render({ accounts_supply_keys: true });

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

  /**
   * The billing sentence is a claim about someone's money. Where the operator
   * supplies the keys, telling the reader they are billed for them is worse
   * than saying nothing at all.
   */
  it('does not claim the reader is billed when the operator supplies the keys', async () => {
    await render({ accounts_supply_keys: false });

    const text = fixture.nativeElement.textContent;
    expect(text).not.toContain('billed to your own account');
    expect(text).toContain("this deployment's provider keys");
  });

  it('claims it when the reader does supply them', async () => {
    await render({ accounts_supply_keys: true });
    expect(fixture.nativeElement.textContent).toContain('billed to your own account');
  });
});

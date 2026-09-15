import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { ProviderKeyStatus } from '../../core/models/provider-key.model';
import { ProviderKeyService } from '../../core/services/provider-key.service';
import { IntegrationsApiKeysTabComponent } from './integrations-api-keys-tab.component';

describe('IntegrationsApiKeysTabComponent', () => {
  let fixture: ComponentFixture<IntegrationsApiKeysTabComponent>;
  let providerKeys: any;

  const status = (over: Partial<ProviderKeyStatus> = {}): ProviderKeyStatus => ({
    provider: 'openai',
    configured: false,
    source: '',
    required: true,
    supported: true,
    ...over,
  });

  async function render(statuses: ProviderKeyStatus[]): Promise<void> {
    providerKeys = {
      listStatuses: vi.fn().mockName('listStatuses').mockReturnValue(of(statuses)),
      setKey: vi.fn().mockName('setKey'),
      removeKey: vi.fn().mockName('removeKey'),
    };

    await TestBed.configureTestingModule({
      imports: [IntegrationsApiKeysTabComponent],
      providers: [provideZonelessChangeDetection(), { provide: ProviderKeyService, useValue: providerKeys }],
    }).compileComponents();

    fixture = TestBed.createComponent(IntegrationsApiKeysTabComponent);
    fixture.detectChanges();
  }

  it('explains what OpenAI is used for while the key is still missing', async () => {
    await render([status()]);

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Add an OpenAI key to finish setting up');
    expect(text).toContain('memory and scratchpad updates');
  });

  /**
   * The reason OpenAI is required does not stop being true once a key is saved
   * — it is the answer to "why is this one required when I chat with Claude",
   * and it is what tells the user whose bill an image lands on. Hiding it after
   * setup is the regression this pins.
   */
  it('keeps explaining what OpenAI is used for after the key is saved', async () => {
    await render([status({ configured: true, source: 'account', key_hint: '…nGQA' })]);

    const text = fixture.nativeElement.textContent;
    expect(text).toContain('An OpenAI key is always required');
    expect(text).toContain('memory and scratchpad updates');
    expect(text).not.toContain('finish setting up');
  });
});

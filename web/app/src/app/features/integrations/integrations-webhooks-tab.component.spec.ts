import type { MockedObject } from 'vitest';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { By } from '@angular/platform-browser';
import { of } from 'rxjs';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { WebhookTokenService } from '../../core/services/webhook-token.service';
import { WebhookScope, WebhookToken } from '../../core/models/webhook-token.model';
import { HelpHintComponent } from '../../shared/ui/help-hint/help-hint.component';
import { IntegrationsWebhooksTabComponent } from './integrations-webhooks-tab.component';

describe('IntegrationsWebhooksTabComponent', () => {
  let fixture: ComponentFixture<IntegrationsWebhooksTabComponent>;
  let component: IntegrationsWebhooksTabComponent;
  let tokenService: Pick<MockedObject<WebhookTokenService>, 'listWebhookTokens' | 'createWebhookToken' | 'revokeWebhookToken'>;
  let confirmationService: Pick<MockedObject<ConfirmationService>, 'confirm' | 'alert'>;

  function makeToken(name: string, scopes: WebhookScope[]): WebhookToken {
    return {
      id: `tok-${name}`,
      user_id: 'user-1',
      name,
      status: 'active',
      scopes,
      created_at: '2026-09-30T00:00:00Z',
      updated_at: '2026-09-30T00:00:00Z'
    };
  }

  async function render(existing: WebhookToken[] = []) {
    tokenService.listWebhookTokens.mockReturnValue(of(existing));
    fixture = TestBed.createComponent(IntegrationsWebhooksTabComponent);
    component = fixture.componentInstance;
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  function checkbox(scope: WebhookScope): HTMLInputElement {
    return fixture.nativeElement.querySelector(`[data-testid="webhook-scope-${scope}"]`) as HTMLInputElement;
  }

  function click(scope: WebhookScope) {
    const box = checkbox(scope);
    box.checked = !box.checked;
    box.dispatchEvent(new Event('change'));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    tokenService = {
      listWebhookTokens: vi.fn().mockName('WebhookTokenService.listWebhookTokens'),
      createWebhookToken: vi.fn().mockName('WebhookTokenService.createWebhookToken'),
      revokeWebhookToken: vi.fn().mockName('WebhookTokenService.revokeWebhookToken')
    };
    tokenService.createWebhookToken.mockReturnValue(
      of({ token: makeToken('new', ['messages:write']), api_token: 'wht_secret' })
    );
    confirmationService = {
      confirm: vi.fn().mockName('ConfirmationService.confirm'),
      alert: vi.fn().mockName('ConfirmationService.alert')
    };
    confirmationService.alert.mockResolvedValue();

    await TestBed.configureTestingModule({
      imports: [IntegrationsWebhooksTabComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: WebhookTokenService, useValue: tokenService },
        { provide: ConfirmationService, useValue: confirmationService }
      ]
    }).compileComponents();
  });

  it('defaults a new token to posting only, so reading is something you choose', async () => {
    await render();

    expect(checkbox('messages:write').checked).toBe(true);
    expect(checkbox('chat:read').checked).toBe(false);

    component.webhookTokenName.set('Slack trigger');
    await component.createWebhookToken();

    expect(tokenService.createWebhookToken).toHaveBeenCalledWith({
      name: 'Slack trigger',
      scopes: ['messages:write']
    });
  });

  it('sends exactly the scopes that are ticked, in a stable order', async () => {
    await render();
    component.webhookTokenName.set('Bridge');

    click('chat:read');
    await component.createWebhookToken();
    expect(tokenService.createWebhookToken).toHaveBeenLastCalledWith({
      name: 'Bridge',
      scopes: ['messages:write', 'chat:read']
    });

    // Read only. The selection went back to posting-only after the create above, so untick posting
    // and tick reading (in the opposite order to the list).
    component.webhookTokenName.set('Reader');
    click('messages:write');
    click('chat:read');
    await component.createWebhookToken();
    expect(tokenService.createWebhookToken).toHaveBeenLastCalledWith({
      name: 'Reader',
      scopes: ['chat:read']
    });
  });

  it('will not create a token that can do nothing', async () => {
    await render();
    component.webhookTokenName.set('Nothing');

    click('messages:write');

    expect(component.canCreateWebhookToken()).toBe(false);
    expect(fixture.nativeElement.querySelector('[data-testid="webhook-scope-required"]')).not.toBeNull();
    await component.createWebhookToken();
    expect(tokenService.createWebhookToken).not.toHaveBeenCalled();
  });

  it('goes back to the posting-only default after a token is created', async () => {
    await render();
    component.webhookTokenName.set('One');
    click('chat:read');

    await component.createWebhookToken();
    fixture.detectChanges();

    expect(checkbox('chat:read').checked).toBe(false);
    expect(checkbox('messages:write').checked).toBe(true);
    expect(component.justCreatedToken()).toBe('wht_secret');
  });

  it('shows what each existing token can do, including tokens from before scopes', async () => {
    await render([makeToken('old poster', ['messages:write']), makeToken('reader', ['chat:read'])]);

    const rows = Array.from(fixture.nativeElement.querySelectorAll('[data-testid="webhook-token-row"]')) as HTMLElement[];
    expect(rows).toHaveLength(2);
    const chips = (row: HTMLElement) =>
      Array.from(row.querySelectorAll('[data-testid="webhook-token-scope"]')).map((el) => (el as HTMLElement).textContent?.trim());

    expect(chips(rows[0])).toEqual(['Post messages']);
    expect(chips(rows[1])).toEqual(['Read threads']);
  });

  it('links the token help hint to the webhooks guide', async () => {
    await render();

    const hint = fixture.debugElement.query(By.directive(HelpHintComponent));
    expect(hint.componentInstance.guide()).toBe('webhooks');

    (hint.nativeElement.querySelector('button') as HTMLButtonElement).click();
    fixture.detectChanges();
    const link = hint.nativeElement.querySelector('.ui-help-hint__link') as HTMLAnchorElement;
    expect(link.getAttribute('href')).toMatch(/\/guides\/webhooks\.html$/);
    expect(link.textContent).toContain('Read the webhooks guide');
  });

  it('describes an active token by its scopes', async () => {
    await render();

    expect(component.activeTokenHint(makeToken('a', ['chat:read']))).toContain('Read your threads');
    expect(component.activeTokenHint(makeToken('b', ['messages:write']))).toContain('Post messages');
    expect(component.activeTokenHint(makeToken('c', []))).toBe('Active');
  });
});

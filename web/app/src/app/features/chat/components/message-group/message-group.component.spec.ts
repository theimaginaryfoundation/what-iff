import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideMarkdown } from 'ngx-markdown';

import { ChatMessage } from '../../../../core/models/message.model';
import { ProviderKeysService } from '../../../../core/services/provider-keys.service';
import { ProfileSettingsModalService } from '../../../profile-settings/profile-settings-modal.service';
import { MessageGroupComponent } from './message-group.component';

function userMessage(overrides: Partial<ChatMessage>): ChatMessage {
  return {
    id: 'msg-1',
    chat_id: 'chat-1',
    message: 'Hey! Can you help me plan a trip?',
    origin: 'User',
    read_status: 'read',
    sent_at: '2026-10-30T12:00:00Z',
    ...overrides,
  } as ChatMessage;
}

describe('MessageGroupComponent failed turns', () => {
  let fixture: ComponentFixture<MessageGroupComponent>;
  let settings: ProfileSettingsModalService;
  const accountsSupplyKeys = signal(true);

  function render(message: ChatMessage): HTMLElement {
    fixture = TestBed.createComponent(MessageGroupComponent);
    fixture.componentRef.setInput('origin', 'User');
    fixture.componentRef.setInput('messages', [message]);
    fixture.detectChanges();
    return fixture.nativeElement.querySelector('[role="alert"]') as HTMLElement;
  }

  function button(alert: HTMLElement, label: string): HTMLButtonElement | undefined {
    return [...alert.querySelectorAll('button')].find(b => b.textContent?.trim() === label);
  }

  beforeEach(async () => {
    accountsSupplyKeys.set(true);
    await TestBed.configureTestingModule({
      imports: [MessageGroupComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideHttpClient(withXhr()),
        provideHttpClientTesting(),
        provideMarkdown(),
        ProfileSettingsModalService,
        { provide: ProviderKeysService, useValue: { accountsSupplyKeys } },
      ],
    }).compileComponents();
    settings = TestBed.inject(ProfileSettingsModalService);
  });

  it('offers to add a key for a turn that failed without one', () => {
    const alert = render(userMessage({ last_error_message: 'No OpenAI API key is set.', last_error_code: 'provider_not_configured' }));
    expect(alert.textContent).toContain('No OpenAI API key is set.');
    button(alert, 'Add key')!.click();
    expect(settings.activeTab()).toBe('providers');
    expect(button(alert, 'Choose models')).toBeUndefined();
  });

  // ADR 0x027: the API states the fact; the app says what to do about it.
  it('says what to do for a turn with no model, and opens the chooser', () => {
    const alert = render(userMessage({ last_error_message: 'no model is selected for this account', last_error_code: 'no_model_selected' }));
    expect(alert.textContent).toContain("No model to reply with yet. Choose which of your providers' models to chat with, then retry.");
    expect(alert.textContent).not.toContain('no model is selected for this account');
    button(alert, 'Choose models')!.click();
    expect(settings.openState()).toBe(true);
    expect(settings.activeTab()).toBe('providers');
    expect(settings.modelChooserRequest()).toBe('');
    expect(button(alert, 'Retry')).toBeTruthy();
  });

  it('shows any other failure as the server stated it', () => {
    const alert = render(userMessage({ last_error_message: 'The provider is overloaded.' }));
    expect(alert.textContent).toContain('The provider is overloaded.');
    expect(button(alert, 'Add key')).toBeUndefined();
    expect(button(alert, 'Choose models')).toBeUndefined();
  });
});

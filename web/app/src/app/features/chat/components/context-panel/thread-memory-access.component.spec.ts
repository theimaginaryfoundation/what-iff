import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, Subject, throwError } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { ChatService } from '../../../../core/services/chat.service';
import { RightPanelService } from '../../../../core/services/right-panel.service';
import { RESTRICTED_WITHHELD_COPY } from '../../helpers/memory-access.helpers';
import { ContextPanelService } from '../../services/context-panel.service';
import { ThreadMemoryAccessComponent } from './thread-memory-access.component';

function makeChat(partial: Partial<Chat> = {}): Chat {
  return { id: 'chat-1', user_id: 'user-1', name: 'Thread', created_at: '', updated_at: '', ...partial };
}

describe('ThreadMemoryAccessComponent', () => {
  let fixture: ComponentFixture<ThreadMemoryAccessComponent>;
  let context: ContextPanelService;
  let chatService: { patchChat: ReturnType<typeof vi.fn> };

  const host = () => fixture.nativeElement as HTMLElement;
  const radios = () => Array.from(host().querySelectorAll('input[type="radio"]')) as HTMLInputElement[];
  const checked = () => radios().find(r => r.checked)?.value;

  async function settle(): Promise<void> {
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    chatService = { patchChat: vi.fn() };
    await TestBed.configureTestingModule({
      imports: [ThreadMemoryAccessComponent],
      providers: [
        provideZonelessChangeDetection(),
        ContextPanelService,
        { provide: RightPanelService, useValue: { setVisible: vi.fn() } },
        { provide: ChatService, useValue: chatService },
      ],
    }).compileComponents();
    context = TestBed.inject(ContextPanelService);
    fixture = TestBed.createComponent(ThreadMemoryAccessComponent);
  });

  it('renders nothing without an active chat', () => {
    fixture.detectChanges();
    expect(host().querySelector('[role="radiogroup"]')).toBeNull();
  });

  it('offers Everything, Not sensitive and Public memories only, defaulting to Everything', () => {
    context.setActiveChat(makeChat());
    fixture.detectChanges();

    expect(Array.from(host().querySelectorAll('.access__label')).map(el => el.textContent?.trim())).toEqual([
      'Everything',
      'Not sensitive',
      'Public memories only',
    ]);
    expect(radios().map(r => r.value)).toEqual(['sensitive', 'personal', 'public']);
    expect(checked()).toBe('sensitive');
  });

  it('shows no sandbox note while unrestricted', () => {
    context.setActiveChat(makeChat());
    fixture.detectChanges();
    expect(host().querySelector('.access__note')).toBeNull();
  });

  it.each(['personal', 'public'] as const)('reflects a stored limit of %s and explains what is withheld', limit => {
    context.setActiveChat(makeChat({ memory_sensitivity_limit: limit }));
    fixture.detectChanges();

    expect(checked()).toBe(limit);
    expect(host().querySelector('.access__note')?.textContent?.trim()).toBe(RESTRICTED_WITHHELD_COPY);
  });

  it('persists the chosen limit with the chat PATCH and publishes the saved chat', async () => {
    context.setActiveChat(makeChat());
    const saved = makeChat({ memory_sensitivity_limit: 'personal' });
    chatService.patchChat.mockReturnValue(of(saved));
    fixture.detectChanges();

    radios()
      .find(r => r.value === 'personal')!
      .click();
    await settle();

    expect(chatService.patchChat).toHaveBeenCalledTimes(1);
    expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { memory_sensitivity_limit: 'personal' });
    expect(context.threadUpdate()).toBe(saved);
    expect(context.activeChat()).toBe(saved);
    expect(checked()).toBe('personal');
    expect(host().querySelector('.access__note')).not.toBeNull();
  });

  it('can lift the restriction again', async () => {
    context.setActiveChat(makeChat({ memory_sensitivity_limit: 'public' }));
    chatService.patchChat.mockReturnValue(of(makeChat({ memory_sensitivity_limit: 'sensitive' })));
    fixture.detectChanges();

    radios()
      .find(r => r.value === 'sensitive')!
      .click();
    await settle();

    expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { memory_sensitivity_limit: 'sensitive' });
    expect(host().querySelector('.access__note')).toBeNull();
  });

  it('does not call the API when the current level is picked again', async () => {
    context.setActiveChat(makeChat({ memory_sensitivity_limit: 'personal' }));
    fixture.detectChanges();

    await fixture.componentInstance.select('personal');

    expect(chatService.patchChat).not.toHaveBeenCalled();
  });

  it('shows an error and keeps the previous level when saving fails', async () => {
    context.setActiveChat(makeChat());
    chatService.patchChat.mockReturnValue(throwError(() => new Error('nope')));
    fixture.detectChanges();

    radios()
      .find(r => r.value === 'public')!
      .click();
    await settle();

    expect(host().querySelector('.access__error')?.textContent).toContain('nope');
    expect(context.threadUpdate()).toBeNull();
    expect(checked()).toBe('sensitive');
  });

  it('disables the options while a save is in flight', async () => {
    context.setActiveChat(makeChat());
    const pending = new Subject<Chat>();
    chatService.patchChat.mockReturnValue(pending);
    fixture.detectChanges();

    void fixture.componentInstance.select('public');
    fixture.detectChanges();
    expect(radios().every(r => r.disabled)).toBe(true);

    pending.next(makeChat({ memory_sensitivity_limit: 'public' }));
    pending.complete();
    await settle();
    expect(radios().some(r => r.disabled)).toBe(false);
  });
});

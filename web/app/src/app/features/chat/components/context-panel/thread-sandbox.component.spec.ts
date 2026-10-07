import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { ChatService } from '../../../../core/services/chat.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { RightPanelService } from '../../../../core/services/right-panel.service';
import { ContextPanelService } from '../../services/context-panel.service';
import { SANDBOX_COPY, SANDBOX_CREATE_ONLY_NOTE, ThreadSandboxComponent } from './thread-sandbox.component';

function makeChat(partial: Partial<Chat> = {}): Chat {
  return {
    id: 'chat-1',
    user_id: 'user-1',
    name: 'Thread',
    personality_id: 'p-1',
    model_id: 'm-1',
    created_at: '',
    updated_at: '',
    ...partial,
  };
}

describe('ThreadSandboxComponent', () => {
  let fixture: ComponentFixture<ThreadSandboxComponent>;
  let context: ContextPanelService;
  let chatService: { patchChat: ReturnType<typeof vi.fn>; createChat: ReturnType<typeof vi.fn>; setLastChatId: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };
  let navigate: ReturnType<typeof vi.fn>;

  const host = () => fixture.nativeElement as HTMLElement;
  const button = (id: string) => host().querySelector(`[data-testid=${id}] button`) as HTMLButtonElement | null;

  async function settle(): Promise<void> {
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    chatService = { patchChat: vi.fn(), createChat: vi.fn(), setLastChatId: vi.fn() };
    confirmation = { confirm: vi.fn().mockResolvedValue(true) };
    await TestBed.configureTestingModule({
      imports: [ThreadSandboxComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        ContextPanelService,
        { provide: RightPanelService, useValue: { setVisible: vi.fn() } },
        { provide: ChatService, useValue: chatService },
        { provide: ConfirmationService, useValue: confirmation },
      ],
    }).compileComponents();
    context = TestBed.inject(ContextPanelService);
    navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true) as unknown as ReturnType<typeof vi.fn>;
    fixture = TestBed.createComponent(ThreadSandboxComponent);
  });

  it('renders nothing without an active chat', () => {
    fixture.detectChanges();
    expect(host().querySelector('.sandbox')).toBeNull();
  });

  it('is off by default, says a thread is sandboxed only at creation, and offers a new sandboxed thread', () => {
    context.setActiveChat(makeChat());
    fixture.detectChanges();

    expect(host().querySelector('.sandbox__label')?.textContent?.trim()).toBe('Sandbox');
    expect(host().querySelector('.sandbox__note')?.textContent?.trim()).toBe(SANDBOX_CREATE_ONLY_NOTE);
    expect(button('sandbox-new-thread')).not.toBeNull();
    expect(button('sandbox-leave')).toBeNull();
  });

  it('reflects a sandboxed chat, explains what it can and cannot do, and offers the way out', () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    fixture.detectChanges();

    expect(host().querySelector('.sandbox__label')?.textContent?.trim()).toBe('Sandboxed thread');
    expect(host().querySelector('.sandbox__note')?.textContent?.trim()).toBe(SANDBOX_COPY);
    expect(button('sandbox-leave')).not.toBeNull();
    expect(button('sandbox-new-thread')).toBeNull();
  });

  it('starts a new sandboxed thread with the same personality and model, and opens it', async () => {
    context.setActiveChat(makeChat());
    chatService.createChat.mockReturnValue(of(makeChat({ id: 'chat-2', context_scope: 'sandbox' })));
    fixture.detectChanges();

    button('sandbox-new-thread')!.click();
    await settle();

    expect(chatService.createChat).toHaveBeenCalledWith({
      name: 'New Chat',
      personality_id: 'p-1',
      model_id: 'm-1',
      context_scope: 'sandbox',
    });
    expect(chatService.setLastChatId).toHaveBeenCalledWith('chat-2');
    expect(navigate).toHaveBeenCalledWith(['/chat', 'chat-2']);
    expect(chatService.patchChat).not.toHaveBeenCalled();
  });

  it('leaving the sandbox asks for confirmation and saves context_scope=account once confirmed', async () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    const saved = makeChat({ context_scope: 'account' });
    chatService.patchChat.mockReturnValue(of(saved));
    fixture.detectChanges();

    button('sandbox-leave')!.click();
    await settle();

    expect(confirmation.confirm).toHaveBeenCalledTimes(1);
    expect(confirmation.confirm.mock.calls[0][0]).toMatchObject({ title: 'Turn off sandbox?', type: 'warning' });
    expect(confirmation.confirm.mock.calls[0][0].message).toContain("can't be undone");
    expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { context_scope: 'account' });
    expect(context.threadUpdate()).toBe(saved);
    expect(context.activeChat()).toBe(saved);
    expect(button('sandbox-new-thread')).not.toBeNull();
  });

  it('keeps the sandbox and saves nothing when the confirmation is declined', async () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    confirmation.confirm.mockResolvedValue(false);
    fixture.detectChanges();

    button('sandbox-leave')!.click();
    await settle();

    expect(confirmation.confirm).toHaveBeenCalledTimes(1);
    expect(chatService.patchChat).not.toHaveBeenCalled();
    expect(context.threadUpdate()).toBeNull();
    expect(button('sandbox-leave')).not.toBeNull();
    expect(fixture.componentInstance.saving()).toBe(false);
  });

  it('shows an error and keeps the sandbox when leaving fails', async () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    chatService.patchChat.mockReturnValue(throwError(() => new Error('nope')));
    fixture.detectChanges();

    button('sandbox-leave')!.click();
    await settle();

    expect(host().querySelector('.sandbox__error')?.textContent).toContain('nope');
    expect(context.threadUpdate()).toBeNull();
    expect(button('sandbox-leave')).not.toBeNull();
  });

  it('shows an error when the new thread cannot be created', async () => {
    context.setActiveChat(makeChat());
    chatService.createChat.mockReturnValue(throwError(() => new Error('no thread')));
    fixture.detectChanges();

    button('sandbox-new-thread')!.click();
    await settle();

    expect(host().querySelector('.sandbox__error')?.textContent).toContain('no thread');
    expect(navigate).not.toHaveBeenCalled();
  });

  it('disables the action while a save is in flight', async () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    const pending = new Subject<Chat>();
    chatService.patchChat.mockReturnValue(pending);
    fixture.detectChanges();

    void fixture.componentInstance.leaveSandbox();
    await settle();
    expect(button('sandbox-leave')!.disabled).toBe(true);

    pending.next(makeChat({ context_scope: 'account' }));
    pending.complete();
    await settle();
    expect(fixture.componentInstance.saving()).toBe(false);
  });
});

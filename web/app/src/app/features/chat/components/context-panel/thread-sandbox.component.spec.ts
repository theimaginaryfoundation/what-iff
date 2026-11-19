import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, Subject, throwError } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { ChatService } from '../../../../core/services/chat.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { ThreadListService } from '../../../../core/services/thread-list.service';
import { RightPanelService } from '../../../../core/services/right-panel.service';
import { ContextPanelService } from '../../services/context-panel.service';
import { SANDBOX_COPY, ThreadSandboxComponent } from './thread-sandbox.component';

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
  let threadList: { applyChatUpdate: ReturnType<typeof vi.fn> };

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
    threadList = { applyChatUpdate: vi.fn() };
    await TestBed.configureTestingModule({
      imports: [ThreadSandboxComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideRouter([]),
        ContextPanelService,
        { provide: RightPanelService, useValue: { setVisible: vi.fn() } },
        { provide: ChatService, useValue: chatService },
        { provide: ConfirmationService, useValue: confirmation },
        { provide: ThreadListService, useValue: threadList },
      ],
    }).compileComponents();
    context = TestBed.inject(ContextPanelService);
    fixture = TestBed.createComponent(ThreadSandboxComponent);
  });

  it('renders nothing without an active chat', () => {
    fixture.detectChanges();
    expect(host().querySelector('.sandbox')).toBeNull();
  });

  it('shows nothing for a thread that is not sandboxed, since it cannot be sandboxed again', () => {
    context.setActiveChat(makeChat());
    fixture.detectChanges();

    expect(host().querySelector('.sandbox')).toBeNull();
    expect(button('sandbox-leave')).toBeNull();
    expect(host().textContent?.trim()).toBe('');
  });

  it('reflects a sandboxed chat, explains what it can and cannot do, and offers the way out', () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    fixture.detectChanges();

    expect(host().querySelector('.sandbox__label')?.textContent?.trim()).toBe('Sandboxed thread');
    expect(host().querySelector('.sandbox__note')).toBeNull();
    const rows = Array.from(host().querySelectorAll('.sandbox__sees-row')).map(r => r.textContent?.replace(/\s+/g, ' ').trim());
    expect(rows).toContain('Your memoriesThis thread only');
    expect(rows).toContain('Other conversationsHidden');
    expect(host().querySelectorAll('.sandbox__sees-value--closed').length).toBe(5);
    expect(SANDBOX_COPY).toContain('A sandboxed thread');
    expect(button('sandbox-leave')).not.toBeNull();
  });

  it('leaving the sandbox asks for confirmation and saves context_scope=account once confirmed', async () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    const saved = makeChat({ context_scope: 'account' });
    chatService.patchChat.mockReturnValue(of(saved));
    fixture.detectChanges();

    button('sandbox-leave')!.click();
    await settle();

    expect(confirmation.confirm).toHaveBeenCalledTimes(1);
    expect(confirmation.confirm.mock.calls[0][0]).toMatchObject({ title: 'Remove from sandbox?', type: 'warning' });
    expect(confirmation.confirm.mock.calls[0][0].message).toContain("can't be undone");
    expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { context_scope: 'account' });
    expect(context.threadUpdate()).toBe(saved);
    expect(context.activeChat()).toBe(saved);
    // The sidebar list is told too, so the row loses its shield.
    expect(threadList.applyChatUpdate).toHaveBeenCalledWith(saved);
    // Out of the sandbox for good: the box goes away, and nothing offers a way back in.
    expect(host().querySelector('.sandbox')).toBeNull();
  });

  it('keeps the sandbox and saves nothing when the confirmation is declined', async () => {
    context.setActiveChat(makeChat({ context_scope: 'sandbox' }));
    confirmation.confirm.mockResolvedValue(false);
    fixture.detectChanges();

    button('sandbox-leave')!.click();
    await settle();

    expect(confirmation.confirm).toHaveBeenCalledTimes(1);
    expect(chatService.patchChat).not.toHaveBeenCalled();
    expect(threadList.applyChatUpdate).not.toHaveBeenCalled();
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

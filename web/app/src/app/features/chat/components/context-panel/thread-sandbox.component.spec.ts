import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of, Subject, throwError } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { ChatService } from '../../../../core/services/chat.service';
import { ConfirmationService } from '../../../../core/services/confirmation.service';
import { RightPanelService } from '../../../../core/services/right-panel.service';
import { ContextPanelService } from '../../services/context-panel.service';
import { SANDBOX_COPY, ThreadSandboxComponent } from './thread-sandbox.component';

function makeChat(partial: Partial<Chat> = {}): Chat {
  return { id: 'chat-1', user_id: 'user-1', name: 'Thread', created_at: '', updated_at: '', ...partial };
}

describe('ThreadSandboxComponent', () => {
  let fixture: ComponentFixture<ThreadSandboxComponent>;
  let context: ContextPanelService;
  let chatService: { patchChat: ReturnType<typeof vi.fn> };
  let confirmation: { confirm: ReturnType<typeof vi.fn> };

  const host = () => fixture.nativeElement as HTMLElement;
  const toggle = () => host().querySelector('input[type="checkbox"]') as HTMLInputElement;

  async function settle(): Promise<void> {
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
  }

  beforeEach(async () => {
    chatService = { patchChat: vi.fn() };
    confirmation = { confirm: vi.fn().mockResolvedValue(true) };
    await TestBed.configureTestingModule({
      imports: [ThreadSandboxComponent],
      providers: [
        provideZonelessChangeDetection(),
        ContextPanelService,
        { provide: RightPanelService, useValue: { setVisible: vi.fn() } },
        { provide: ChatService, useValue: chatService },
        { provide: ConfirmationService, useValue: confirmation },
      ],
    }).compileComponents();
    context = TestBed.inject(ContextPanelService);
    fixture = TestBed.createComponent(ThreadSandboxComponent);
  });

  it('renders nothing without an active chat', () => {
    fixture.detectChanges();
    expect(toggle()).toBeNull();
  });

  it('is off by default and says so', () => {
    context.setActiveChat(makeChat());
    fixture.detectChanges();

    expect(host().querySelector('.sandbox__label')?.textContent?.trim()).toBe('Sandbox');
    expect(toggle().checked).toBe(false);
    expect(host().querySelector('.sandbox__note')?.textContent).toContain('Off');
  });

  it('reflects a sandboxed chat and explains what it can and cannot see', () => {
    context.setActiveChat(makeChat({ sandboxed: true }));
    fixture.detectChanges();

    expect(toggle().checked).toBe(true);
    expect(host().querySelector('.sandbox__note')?.textContent?.trim()).toBe(SANDBOX_COPY);
  });

  it('turning it on saves sandboxed=true without asking for confirmation and publishes the saved chat', async () => {
    context.setActiveChat(makeChat());
    const saved = makeChat({ sandboxed: true });
    chatService.patchChat.mockReturnValue(of(saved));
    fixture.detectChanges();

    toggle().click();
    await settle();

    expect(confirmation.confirm).not.toHaveBeenCalled();
    expect(chatService.patchChat).toHaveBeenCalledTimes(1);
    expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { sandboxed: true });
    expect(context.threadUpdate()).toBe(saved);
    expect(context.activeChat()).toBe(saved);
    expect(toggle().checked).toBe(true);
    expect(host().querySelector('.sandbox__note')?.textContent?.trim()).toBe(SANDBOX_COPY);
  });

  it('turning it off asks for confirmation and saves sandboxed=false once confirmed', async () => {
    context.setActiveChat(makeChat({ sandboxed: true }));
    chatService.patchChat.mockReturnValue(of(makeChat({ sandboxed: false })));
    fixture.detectChanges();

    toggle().click();
    await settle();

    expect(confirmation.confirm).toHaveBeenCalledTimes(1);
    expect(confirmation.confirm.mock.calls[0][0]).toMatchObject({ title: 'Turn off sandbox?', type: 'warning' });
    expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { sandboxed: false });
    expect(toggle().checked).toBe(false);
    expect(host().querySelector('.sandbox__note')?.textContent).toContain('Off');
  });

  it('keeps the sandbox on and saves nothing when the confirmation is declined', async () => {
    context.setActiveChat(makeChat({ sandboxed: true }));
    confirmation.confirm.mockResolvedValue(false);
    fixture.detectChanges();

    toggle().click();
    await settle();

    expect(confirmation.confirm).toHaveBeenCalledTimes(1);
    expect(chatService.patchChat).not.toHaveBeenCalled();
    expect(context.threadUpdate()).toBeNull();
    expect(toggle().checked).toBe(true);
    expect(fixture.componentInstance.saving()).toBe(false);
  });

  it('does not call the API when the current value is chosen again', async () => {
    context.setActiveChat(makeChat({ sandboxed: true }));
    fixture.detectChanges();

    await fixture.componentInstance.toggle(true);

    expect(confirmation.confirm).not.toHaveBeenCalled();
    expect(chatService.patchChat).not.toHaveBeenCalled();
  });

  it('shows an error and keeps the previous state when saving fails', async () => {
    context.setActiveChat(makeChat());
    chatService.patchChat.mockReturnValue(throwError(() => new Error('nope')));
    fixture.detectChanges();

    toggle().click();
    await settle();

    expect(host().querySelector('.sandbox__error')?.textContent).toContain('nope');
    expect(context.threadUpdate()).toBeNull();
    expect(toggle().checked).toBe(false);
  });

  it('disables the toggle while a save is in flight', async () => {
    context.setActiveChat(makeChat());
    const pending = new Subject<Chat>();
    chatService.patchChat.mockReturnValue(pending);
    fixture.detectChanges();

    void fixture.componentInstance.toggle(true);
    fixture.detectChanges();
    expect(toggle().disabled).toBe(true);

    pending.next(makeChat({ sandboxed: true }));
    pending.complete();
    await settle();
    expect(toggle().disabled).toBe(false);
  });
});

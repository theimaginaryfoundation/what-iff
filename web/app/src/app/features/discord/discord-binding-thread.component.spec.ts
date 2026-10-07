import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { DiscordBindingThreadComponent } from './discord-binding-thread.component';
import { DiscordBinding, DiscordService } from './discord.service';

const binding: DiscordBinding = {
  id: 'b1',
  bot_id: 'bot',
  chat_id: 'chat-current',
  guild_id: 'g',
  channel_id: 'c',
  guild_name: 'Home',
  channel_name: 'general',
  inbound_enabled: true,
  allow_user_ids: [],
  deny_user_ids: [],
  allow_unrestricted: false,
  chat_sandboxed: false,
  status: 'active',
  created_at: '',
  updated_at: '',
};

async function settle(fixture: ComponentFixture<unknown>): Promise<void> {
  for (let i = 0; i < 3; i++) await new Promise(r => setTimeout(r, 0));
  fixture.detectChanges();
}

describe('DiscordBindingThreadComponent', () => {
  let fixture: ComponentFixture<DiscordBindingThreadComponent>;
  let discord: { updateBinding: ReturnType<typeof vi.fn> };
  let chatService: { listChatsPage: ReturnType<typeof vi.fn>; createChat: ReturnType<typeof vi.fn> };
  const q = (id: string) => fixture.nativeElement.querySelector(`[data-testid=${id}]`) as HTMLElement | null;

  beforeEach(async () => {
    discord = { updateBinding: vi.fn((_id: string, body: Partial<DiscordBinding>) => of({ ...binding, ...body, chat_sandboxed: true })) };
    chatService = {
      listChatsPage: vi.fn(() =>
        of({
          results: [
            { id: 'chat-current', name: 'Current relay thread', context_scope: 'sandbox' },
            { id: 'chat-open', name: 'My main thread', context_scope: 'account' },
            { id: 'chat-safe', name: 'Spare sandbox', context_scope: 'sandbox' },
          ],
          total_count: 3,
          page: 1,
        }),
      ),
      createChat: vi.fn(() => of({ id: 'chat-new', name: 'Discord · #general', context_scope: 'sandbox' })),
    };
    await TestBed.configureTestingModule({
      imports: [DiscordBindingThreadComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: DiscordService, useValue: discord },
        { provide: ChatService, useValue: chatService },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(DiscordBindingThreadComponent);
    fixture.componentRef.setInput('binding', binding);
    fixture.componentRef.setInput('personalityId', 'p1');
    fixture.detectChanges();
  });

  async function openForm(): Promise<void> {
    q('discord-binding-move')!.click();
    await settle(fixture);
  }

  it('offers the persona’s other threads, with a new sandboxed thread as the default', async () => {
    await openForm();
    expect(chatService.listChatsPage).toHaveBeenCalledWith(1, 50, { personality_id: 'p1' });
    const options = Array.from(q('discord-binding-move-select')!.querySelectorAll('option')).map(o => o.textContent?.trim());
    expect(options[0]).toBe('A new sandboxed thread (recommended)');
    expect(options).toContain('My main thread (not sandboxed)');
    expect(options).toContain('Spare sandbox (sandboxed)');
    expect(options).not.toContain('Current relay thread (sandboxed)');
    expect(q('discord-binding-move-warning')).toBeNull();
  });

  it('moves to a new sandboxed thread named after the channel', async () => {
    await openForm();
    const emitted: DiscordBinding[] = [];
    fixture.componentInstance.moved.subscribe(b => emitted.push(b));

    q('discord-binding-move-confirm')!.click();
    await settle(fixture);

    expect(chatService.createChat).toHaveBeenCalledWith({ name: 'Discord · #general', personality_id: 'p1', context_scope: 'sandbox' });
    expect(discord.updateBinding).toHaveBeenCalledWith('b1', { chat_id: 'chat-new' });
    expect(emitted.map(b => b.chat_id)).toEqual(['chat-new']);
    expect(q('discord-binding-move-form')).toBeNull();
  });

  it('requires the acknowledgement for a thread that is not sandboxed, and sends it', async () => {
    await openForm();
    fixture.componentInstance.pick('chat-open');
    fixture.detectChanges();

    expect(q('discord-binding-move-warning')).not.toBeNull();
    expect((q('discord-binding-move-confirm') as HTMLButtonElement).disabled).toBe(true);

    fixture.componentInstance.ack.set(true);
    fixture.detectChanges();
    expect((q('discord-binding-move-confirm') as HTMLButtonElement).disabled).toBe(false);
    q('discord-binding-move-confirm')!.click();
    await settle(fixture);

    expect(chatService.createChat).not.toHaveBeenCalled();
    expect(discord.updateBinding).toHaveBeenCalledWith('b1', { chat_id: 'chat-open', allow_unrestricted: true });
  });

  it('moves to an existing sandboxed thread without an acknowledgement', async () => {
    await openForm();
    fixture.componentInstance.pick('chat-safe');
    fixture.detectChanges();

    expect(q('discord-binding-move-warning')).toBeNull();
    q('discord-binding-move-confirm')!.click();
    await settle(fixture);
    expect(discord.updateBinding).toHaveBeenCalledWith('b1', { chat_id: 'chat-safe' });
  });

  it('resets the acknowledgement when another thread is picked', async () => {
    await openForm();
    fixture.componentInstance.pick('chat-open');
    fixture.componentInstance.ack.set(true);
    fixture.componentInstance.pick('chat-safe');
    expect(fixture.componentInstance.ack()).toBe(false);
  });
});

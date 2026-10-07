import { signal } from '@angular/core';
import { provideRouter } from '@angular/router';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { ContextPanelService } from '../chat/services/context-panel.service';
import { DiscordComposerToggleComponent } from './discord-composer-toggle.component';
import { DiscordBinding, DiscordChatState, DiscordService } from './discord.service';

const binding: DiscordBinding = {
  id: 'b1',
  bot_id: 'bot',
  chat_id: 'chat-1',
  guild_id: 'g',
  channel_id: 'c',
  guild_name: 'Home',
  channel_name: 'general',
  inbound_enabled: true,
  allow_user_ids: [],
  deny_user_ids: [],
  allow_unrestricted: false,
  status: 'active',
  created_at: '',
  updated_at: '',
};

/** The toggle loads with plain promises, which zoneless whenStable does not track. */
async function settle(): Promise<void> {
  for (let i = 0; i < 3; i++) await new Promise(r => setTimeout(r, 0));
}

describe('DiscordComposerToggleComponent', () => {
  const activeChatId = signal<string | null>('chat-1');
  const latestBreakdown = signal<unknown>(null);
  const activeChat = signal<{ id: string; context_scope?: 'account' | 'sandbox' } | null>({ id: 'chat-1', context_scope: 'sandbox' });
  let state: DiscordChatState;
  let setPostNextReply: ReturnType<typeof vi.fn>;

  async function create(enabled = true) {
    setPostNextReply = vi.fn(() => of(undefined));
    TestBed.configureTestingModule({
      imports: [DiscordComposerToggleComponent],
      providers: [
        provideRouter([]),
        { provide: ContextPanelService, useValue: { activeChatId, latestBreakdown, activeChat } },
        { provide: DiscordService, useValue: { available: () => of(enabled), chatState: () => of(state), setPostNextReply } },
      ],
    });
    const fixture = TestBed.createComponent(DiscordComposerToggleComponent);
    fixture.detectChanges();
    await settle();
    fixture.detectChanges();
    return fixture;
  }

  beforeEach(() => {
    state = { bindings: [binding], pending_binding_ids: [], links: [] };
    activeChat.set({ id: 'chat-1', context_scope: 'sandbox' });
  });

  it('shows a toggle per connected channel and arms the next reply', async () => {
    const fixture = await create();
    const box = fixture.nativeElement.querySelector('input[type=checkbox]') as HTMLInputElement;
    expect(fixture.nativeElement.textContent).toContain('Post the next reply to #general · Home');
    expect(box.checked).toBe(false);

    box.checked = true;
    box.dispatchEvent(new Event('change'));
    await settle();

    expect(setPostNextReply).toHaveBeenCalledWith('chat-1', 'b1', true);
    expect(fixture.componentInstance.pending().has('b1')).toBe(true);
  });

  it('says tags are paused, with a way to Integrations, once the thread leaves the sandbox unacknowledged', async () => {
    activeChat.set({ id: 'chat-1', context_scope: 'account' });
    const fixture = await create();
    const note = fixture.nativeElement.querySelector('[data-testid=discord-paused-note]') as HTMLElement;
    expect(note.textContent).toContain('no longer sandboxed');
    expect(note.textContent).toContain('#general · Home');
    expect(note.querySelector('a')?.getAttribute('href')).toContain('/integrations');

    activeChat.set({ id: 'chat-1', context_scope: 'sandbox' });
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('[data-testid=discord-paused-note]')).toBeNull();
  });

  it('says nothing once the binding is acknowledged', async () => {
    activeChat.set({ id: 'chat-1', context_scope: 'account' });
    state = { bindings: [{ ...binding, allow_unrestricted: true }], pending_binding_ids: [], links: [] };
    const fixture = await create();
    expect(fixture.nativeElement.querySelector('[data-testid=discord-paused-note]')).toBeNull();
  });

  it('reflects a pending post from the server', async () => {
    state.pending_binding_ids = ['b1'];
    const fixture = await create();
    expect((fixture.nativeElement.querySelector('input') as HTMLInputElement).checked).toBe(true);
  });

  it('renders nothing outside a relay thread or when the server does not run the relay', async () => {
    state.bindings = [];
    let fixture = await create();
    expect(fixture.nativeElement.querySelector('[data-testid=discord-composer-toggle]')).toBeNull();

    TestBed.resetTestingModule();
    state.bindings = [binding];
    fixture = await create(false);
    expect(fixture.nativeElement.querySelector('[data-testid=discord-composer-toggle]')).toBeNull();
  });
});

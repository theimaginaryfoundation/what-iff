import { signal } from '@angular/core';
import { provideRouter } from '@angular/router';
import { TestBed } from '@angular/core/testing';
import { of } from 'rxjs';

import { ContextPanelService } from '../chat/services/context-panel.service';
import { BuzzComposerToggleComponent } from './buzz-composer-toggle.component';
import { BuzzBinding, BuzzChatState, BuzzService } from './buzz.service';

const binding: BuzzBinding = {
  id: 'b1',
  community_id: 'c1',
  community_name: 'Acme',
  chat_id: 'chat-1',
  channel_id: 'ch',
  channel_name: 'general',
  inbound_enabled: true,
  allow_pubkeys: [],
  deny_pubkeys: [],
  answer_agents: false,
  allow_unrestricted: false,
  status: 'active',
  created_at: '',
  updated_at: '',
};

/** The toggle loads with plain promises, which zoneless whenStable does not track. */
async function settle(): Promise<void> {
  for (let i = 0; i < 3; i++) await new Promise(r => setTimeout(r, 0));
}

describe('BuzzComposerToggleComponent', () => {
  const activeChatId = signal<string | null>('chat-1');
  const latestBreakdown = signal<unknown>(null);
  const activeChat = signal<{ id: string; context_scope?: 'account' | 'sandbox' } | null>({ id: 'chat-1', context_scope: 'sandbox' });
  let state: BuzzChatState;
  let setPostNextReply: ReturnType<typeof vi.fn>;

  async function create(enabled = true) {
    setPostNextReply = vi.fn(() => of(undefined));
    TestBed.configureTestingModule({
      imports: [BuzzComposerToggleComponent],
      providers: [
        provideRouter([]),
        { provide: ContextPanelService, useValue: { activeChatId, latestBreakdown, activeChat } },
        { provide: BuzzService, useValue: { available: () => of(enabled), chatState: () => of(state), setPostNextReply } },
      ],
    });
    const fixture = TestBed.createComponent(BuzzComposerToggleComponent);
    fixture.detectChanges();
    await settle();
    fixture.detectChanges();
    return fixture;
  }

  beforeEach(() => {
    state = { bindings: [binding], pending_binding_ids: [], links: [] };
    activeChat.set({ id: 'chat-1', context_scope: 'sandbox' });
  });

  it('shows a toggle per connected channel, off by default, and arms the next reply', async () => {
    const fixture = await create();
    const box = fixture.nativeElement.querySelector('input[type=checkbox]') as HTMLInputElement;
    expect(fixture.nativeElement.textContent).toContain('Post the next reply to #general · Acme on Buzz');
    expect(box.checked).toBe(false);

    box.checked = true;
    box.dispatchEvent(new Event('change'));
    await settle();

    expect(setPostNextReply).toHaveBeenCalledWith('chat-1', 'b1', true);
    expect(fixture.componentInstance.pending().has('b1')).toBe(true);
  });

  it('reflects a pending post from the server, and leaves out broken bindings', async () => {
    state = {
      bindings: [binding, { ...binding, id: 'b2', channel_name: 'random', status: 'broken' }],
      pending_binding_ids: ['b1'],
      links: [],
    };
    const fixture = await create();
    const boxes = fixture.nativeElement.querySelectorAll('input[type=checkbox]') as NodeListOf<HTMLInputElement>;
    expect(boxes.length).toBe(1);
    expect(boxes[0].checked).toBe(true);
  });

  it('says mentions are paused, with a link to the Buzz tab, once the thread leaves the sandbox unacknowledged', async () => {
    activeChat.set({ id: 'chat-1', context_scope: 'account' });
    const fixture = await create();
    const note = fixture.nativeElement.querySelector('[data-testid=buzz-paused-note]') as HTMLElement;
    expect(note.textContent).toContain('no longer sandboxed');
    expect(note.querySelector('a')?.getAttribute('href')).toContain('tab=buzz');

    TestBed.resetTestingModule();
    state = { bindings: [{ ...binding, allow_unrestricted: true }], pending_binding_ids: [], links: [] };
    const acknowledged = await create();
    expect(acknowledged.nativeElement.querySelector('[data-testid=buzz-paused-note]')).toBeNull();
  });

  it('renders nothing outside a relay thread or when the server does not run the relay', async () => {
    state.bindings = [];
    let fixture = await create();
    expect(fixture.nativeElement.querySelector('[data-testid=buzz-composer-toggle]')).toBeNull();

    TestBed.resetTestingModule();
    state.bindings = [binding];
    fixture = await create(false);
    expect(fixture.nativeElement.querySelector('[data-testid=buzz-composer-toggle]')).toBeNull();
  });
});

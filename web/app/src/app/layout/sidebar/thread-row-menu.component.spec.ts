import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { Chat } from '../../core/models/chat.model';
import { ThreadRowMenuComponent } from './thread-row-menu.component';

function chat(partial: Partial<Chat> = {}): Chat {
  return { id: 'c1', user_id: 'u', name: 'Trip', personality_id: 'p', created_at: '', updated_at: '', ...partial };
}

describe('ThreadRowMenuComponent', () => {
  let fixture: ComponentFixture<ThreadRowMenuComponent>;
  const host = () => fixture.nativeElement as HTMLElement;
  const q = (id: string) => host().querySelector(`[data-testid=${id}]`) as HTMLButtonElement | null;

  function render(c: Chat): void {
    fixture.componentRef.setInput('thread', c);
    fixture.detectChanges();
  }

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ThreadRowMenuComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(ThreadRowMenuComponent);
  });

  it('is closed until the trigger is clicked, and the trigger names the thread', () => {
    render(chat());
    expect(q('thread-menu-star')).toBeNull();
    expect(q('thread-menu-trigger')!.getAttribute('aria-label')).toBe('Actions for Trip');
    q('thread-menu-trigger')!.click();
    fixture.detectChanges();
    expect(q('thread-menu-star')).not.toBeNull();
    expect(q('thread-menu-trigger')!.getAttribute('aria-expanded')).toBe('true');
  });

  it('offers Star or Unstar from the favorite state and emits the thread', () => {
    const c = chat({ is_favorite: true });
    render(c);
    const spy = vi.spyOn(fixture.componentInstance.toggleStar, 'emit');
    q('thread-menu-trigger')!.click();
    fixture.detectChanges();
    expect(q('thread-menu-star')!.textContent).toContain('Unstar');
    q('thread-menu-star')!.click();
    fixture.detectChanges();
    expect(spy).toHaveBeenCalledWith(c);
    expect(q('thread-menu-star')).toBeNull();
  });

  it('links to the Context, Memories, Tools and Files sections and emits the thread with the tab', () => {
    const c = chat();
    render(c);
    const spy = vi.spyOn(fixture.componentInstance.openPanel, 'emit');
    for (const tab of ['scratchpad', 'memories', 'tools', 'files'] as const) {
      q('thread-menu-trigger')!.click();
      fixture.detectChanges();
      q(`thread-menu-${tab}`)!.click();
      fixture.detectChanges();
      expect(spy).toHaveBeenLastCalledWith({ thread: c, tab });
      expect(q(`thread-menu-${tab}`)).toBeNull();
    }
  });

  it('puts a shield on Memories for a sandboxed thread only', () => {
    render(chat({ context_scope: 'sandbox' }));
    q('thread-menu-trigger')!.click();
    fixture.detectChanges();
    expect(q('thread-menu-memories')!.querySelector('ui-shield-icon')).not.toBeNull();
    expect(q('thread-menu-tools')!.querySelector('ui-shield-icon')).toBeNull();

    render(chat({ context_scope: 'account' }));
    expect(q('thread-menu-memories')!.querySelector('ui-shield-icon')).toBeNull();
  });

  it('ends with Archive, which emits the thread, and has no sandbox item', () => {
    const c = chat({ context_scope: 'sandbox' });
    render(c);
    const spy = vi.spyOn(fixture.componentInstance.archive, 'emit');
    q('thread-menu-trigger')!.click();
    fixture.detectChanges();
    expect(q('thread-menu-sandbox')).toBeNull();
    const items = Array.from(host().querySelectorAll('[role="menuitem"]')).map(i => i.textContent?.trim());
    expect(items).toEqual(['Star', 'Scratchpad', 'Memories', 'Tools', 'Files', 'Archive']);
    q('thread-menu-archive')!.click();
    expect(spy).toHaveBeenCalledWith(c);
  });

  describe('keyboard', () => {
    const items = () => Array.from(host().querySelectorAll<HTMLElement>('[role="menuitem"]'));
    const key = (target: Element, k: string) => {
      target.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
      fixture.detectChanges();
    };

    beforeEach(() => document.body.appendChild(host()));
    afterEach(() => host().remove());

    it('moves focus to the first item on open, then arrows and Home/End move between items', async () => {
      render(chat());
      q('thread-menu-trigger')!.click();
      fixture.detectChanges();
      await fixture.whenStable();
      expect(document.activeElement).toBe(items()[0]);

      const last = () => items()[items().length - 1];
      expect(items().length).toBe(6);

      key(items()[0], 'ArrowDown');
      expect(document.activeElement).toBe(items()[1]);
      key(items()[1], 'ArrowUp');
      expect(document.activeElement).toBe(items()[0]);
      key(items()[0], 'ArrowUp');
      expect(document.activeElement).toBe(last());
      key(last(), 'ArrowDown');
      expect(document.activeElement).toBe(items()[0]);
      key(items()[0], 'End');
      expect(document.activeElement).toBe(last());
      key(last(), 'Home');
      expect(document.activeElement).toBe(items()[0]);
    });

    it('Escape closes the menu and returns focus to the trigger', async () => {
      render(chat());
      q('thread-menu-trigger')!.click();
      fixture.detectChanges();
      await fixture.whenStable();

      key(items()[0], 'Escape');
      expect(q('thread-menu-star')).toBeNull();
      expect(document.activeElement).toBe(q('thread-menu-trigger'));
    });

    it('the menu is labelled with the thread name', () => {
      render(chat());
      q('thread-menu-trigger')!.click();
      fixture.detectChanges();
      expect(host().querySelector('[role="menu"]')!.getAttribute('aria-label')).toBe('Actions for Trip');
    });
  });
});

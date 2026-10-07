import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { DiscordIdChipsComponent } from './discord-id-chips.component';

@Component({
  standalone: true,
  imports: [DiscordIdChipsComponent],
  template: `<app-discord-id-chips [ids]="ids()" (idsChange)="ids.set($event)" />`,
})
class HostComponent {
  readonly ids = signal<string[]>(['111']);
}

describe('DiscordIdChipsComponent', () => {
  function create() {
    const fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
    const el = fixture.nativeElement as HTMLElement;
    const entry = el.querySelector('input') as HTMLInputElement;
    const type = (text: string) => {
      entry.value = text;
      entry.dispatchEvent(new Event('input'));
      fixture.detectChanges();
    };
    const key = (k: string) => {
      entry.dispatchEvent(new KeyboardEvent('keydown', { key: k, cancelable: true }));
      fixture.detectChanges();
    };
    const paste = (text: string) => {
      const event = new Event('paste', { cancelable: true });
      Object.defineProperty(event, 'clipboardData', { value: { getData: () => text } });
      entry.dispatchEvent(event);
      fixture.detectChanges();
      return event;
    };
    const chips = () => Array.from(el.querySelectorAll('.chips__chip')).map(c => c.textContent?.replace('×', '').trim());
    return { fixture, el, entry, type, key, paste, chips };
  }

  it('shows existing ids as chips', () => {
    expect(create().chips()).toEqual(['111']);
  });

  it('adds an id on Enter, and on a typed comma or space', () => {
    const { fixture, entry, type, key, chips } = create();
    type('222');
    key('Enter');
    expect(chips()).toEqual(['111', '222']);
    expect(entry.value).toBe('');

    type('333,');
    expect(chips()).toEqual(['111', '222', '333']);
    type('444 ');
    expect(chips()).toEqual(['111', '222', '333', '444']);
    expect(fixture.componentInstance.ids()).toEqual(['111', '222', '333', '444']);
  });

  it('splits a pasted list of ids on commas, spaces and new lines, ignoring repeats', () => {
    const { paste, entry, chips } = create();
    const event = paste('222, 333\n111\n444');
    expect(event.defaultPrevented).toBe(true);
    expect(chips()).toEqual(['111', '222', '333', '444']);
    expect(entry.value).toBe('');
  });

  it('keeps anything that is not an id in the field with a note', () => {
    const { el, entry, type, chips } = create();
    type('555 gori');
    expect(chips()).toEqual(['111', '555']);
    expect(entry.value).toBe('gori');
    expect(el.querySelector('[role=alert]')?.textContent).toContain('numbers only');

    type('');
    expect(el.querySelector('[role=alert]')).toBeNull();
  });

  it('adds what is typed when the field loses focus', () => {
    const { fixture, entry, type, chips } = create();
    type('222');
    entry.dispatchEvent(new Event('blur'));
    fixture.detectChanges();
    expect(chips()).toEqual(['111', '222']);
  });

  it('removes a chip with its button, and the last one with Backspace on an empty field', () => {
    const { fixture, el, key, chips } = create();
    (el.querySelector('.chips__remove') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(chips()).toEqual([]);

    fixture.componentInstance.ids.set(['1', '2']);
    fixture.detectChanges();
    key('Backspace');
    expect(chips()).toEqual(['1']);
  });
});

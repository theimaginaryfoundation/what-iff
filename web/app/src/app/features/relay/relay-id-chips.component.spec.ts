import { Component, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { BUZZ_PUBKEYS } from '../buzz/buzz.service';
import { DISCORD_USER_IDS } from '../discord/discord.service';
import { RelayIdChipsComponent } from './relay-id-chips.component';

@Component({
  standalone: true,
  imports: [RelayIdChipsComponent],
  template: `<app-relay-id-chips [kind]="kind" [ids]="ids()" (idsChange)="ids.set($event)" />`,
})
class HostComponent {
  readonly kind = DISCORD_USER_IDS;
  readonly ids = signal<string[]>(['111']);
}

describe('RelayIdChipsComponent, with Discord user ids', () => {
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

const HEX = '3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d';
const NPUB = 'npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6';
const HEX2 = '7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e';

describe('RelayIdChipsComponent, with Buzz public keys', () => {
  function create(keys: string[] = []) {
    const fixture = TestBed.createComponent(RelayIdChipsComponent);
    fixture.componentRef.setInput('kind', BUZZ_PUBKEYS);
    fixture.componentRef.setInput('ids', keys);
    const emitted: string[][] = [];
    fixture.componentInstance.idsChange.subscribe(k => emitted.push(k));
    fixture.detectChanges();
    const input = fixture.nativeElement.querySelector('input') as HTMLInputElement;
    return { fixture, emitted, input };
  }

  it('shows keys as shortened npubs with the full npub on hover', () => {
    const { fixture } = create([HEX]);
    const chip = fixture.nativeElement.querySelector('.chips__chip') as HTMLElement;
    expect(chip.textContent).toContain('npub180cvv07…h6w6');
    expect(chip.getAttribute('title')).toBe(NPUB);
  });

  it('adds an npub or hex as hex, without duplicates', () => {
    const { fixture, emitted, input } = create([HEX]);
    input.value = `${NPUB}, ${HEX2.toUpperCase()}`;
    fixture.componentInstance.commit(input);
    expect(emitted).toEqual([[HEX, HEX2]]);
    expect(input.value).toBe('');
  });

  it('keeps what is not a key in the field with a note', () => {
    const { fixture, emitted, input } = create();
    input.value = `alice ${NPUB}`;
    fixture.componentInstance.commit(input);
    fixture.detectChanges();
    expect(emitted).toEqual([[HEX]]);
    expect(input.value).toBe('alice');
    expect(fixture.nativeElement.querySelector('[role=alert]')?.textContent).toContain('npub1');
  });

  it('removes a key', () => {
    const { fixture, emitted } = create([HEX, HEX2]);
    (fixture.nativeElement.querySelector('.chips__remove') as HTMLButtonElement).click();
    expect(emitted).toEqual([[HEX2]]);
  });
});

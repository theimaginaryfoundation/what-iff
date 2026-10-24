import { TestBed } from '@angular/core/testing';

import { BuzzPubkeyChipsComponent } from './buzz-pubkey-chips.component';

const HEX = '3bf0c63fcb93463407af97a5e5ee64fa883d107ef9e558472c4eb9aaaefa459d';
const NPUB = 'npub180cvv07tjdrrgpa0j7j7tmnyl2yr6yr7l8j4s3evf6u64th6gkwsyjh6w6';
const HEX2 = '7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e';

describe('BuzzPubkeyChipsComponent', () => {
  function create(keys: string[] = []) {
    const fixture = TestBed.createComponent(BuzzPubkeyChipsComponent);
    fixture.componentRef.setInput('keys', keys);
    const emitted: string[][] = [];
    fixture.componentInstance.keysChange.subscribe(k => emitted.push(k));
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

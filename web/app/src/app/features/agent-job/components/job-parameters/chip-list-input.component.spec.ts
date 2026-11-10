import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { ChipListInputComponent } from './chip-list-input.component';

describe('ChipListInputComponent', () => {
  let fixture: ComponentFixture<ChipListInputComponent>;
  let emitted: string[][];

  const input = () => fixture.nativeElement.querySelector('input') as HTMLInputElement;
  const type = (text: string, key = 'Enter') => {
    input().value = text;
    input().dispatchEvent(new Event('input'));
    input().dispatchEvent(new KeyboardEvent('keydown', { key }));
  };

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ChipListInputComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(ChipListInputComponent);
    fixture.componentRef.setInput('items', ['AI']);
    emitted = [];
    fixture.componentInstance.itemsChange.subscribe(items => emitted.push(items));
    fixture.detectChanges();
  });

  it('adds a trimmed item on Enter or comma, ignoring blanks and duplicates', () => {
    type('  climate ');
    expect(emitted.at(-1)).toEqual(['AI', 'climate']);
    // No change detection in between, so the input is stale: the second item must still keep the first.
    type('space', ',');
    expect(emitted.at(-1)).toEqual(['AI', 'climate', 'space']);
    type('AI');
    type('   ');
    expect(emitted.length).toBe(2);
  });

  it('removes an item with its button, or the last one with Backspace in an empty box', () => {
    fixture.componentRef.setInput('items', ['AI', 'climate']);
    fixture.detectChanges();
    (fixture.nativeElement.querySelector('button[aria-label="Remove AI"]') as HTMLButtonElement).click();
    expect(emitted.at(-1)).toEqual(['climate']);
    input().dispatchEvent(new KeyboardEvent('keydown', { key: 'Backspace' }));
    expect(emitted.at(-1)).toEqual([]);
    input().dispatchEvent(new KeyboardEvent('keydown', { key: 'Backspace' }));
    expect(emitted.length).toBe(2);
  });
});

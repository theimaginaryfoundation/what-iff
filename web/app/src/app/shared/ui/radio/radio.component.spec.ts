import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { RadioComponent } from './radio.component';

describe('RadioComponent', () => {
  let fixture: ComponentFixture<RadioComponent>;
  let input: HTMLInputElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [RadioComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(RadioComponent);
    fixture.componentRef.setInput('name', 'group');
    fixture.detectChanges();
    input = fixture.nativeElement.querySelector('input');
  });

  it('is a native radio in its group', () => {
    expect(input.type).toBe('radio');
    expect(input.name).toBe('group');
    expect(input.getAttribute('role')).toBeNull();
  });

  it('emits selected when chosen', () => {
    let count = 0;
    fixture.componentInstance.selected.subscribe(() => count++);
    input.click();
    expect(count).toBe(1);
    expect(input.checked).toBe(true);
  });

  it('reflects checked', () => {
    fixture.componentRef.setInput('checked', true);
    fixture.detectChanges();
    expect(input.checked).toBe(true);
  });
});

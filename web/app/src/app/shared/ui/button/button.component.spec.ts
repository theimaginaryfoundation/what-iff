import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { ButtonComponent } from './button.component';

@Component({ standalone: true, imports: [ButtonComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-button variant="primary" (activate)="count = count + 1">Save</ui-button>' })
class ButtonHostComponent { count = 0; }

describe('ButtonComponent', () => {
  let fixture: ComponentFixture<ButtonHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [ButtonHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(ButtonHostComponent);
    fixture.detectChanges();
  });

  it('emits activate on click and keyboard activation', () => {
    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
    button.click();
    button.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    expect(fixture.componentInstance.count).toBe(2);
    expect(button.textContent).toContain('Save');
  });
});

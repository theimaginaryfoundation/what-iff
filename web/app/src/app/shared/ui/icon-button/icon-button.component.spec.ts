import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { IconButtonComponent } from './icon-button.component';

@Component({ standalone: true, imports: [IconButtonComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-icon-button ariaLabel="Close" (activate)="count = count + 1">x</ui-icon-button>' })
class IconButtonHostComponent { count = 0; }

describe('IconButtonComponent', () => {
  let fixture: ComponentFixture<IconButtonHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [IconButtonHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(IconButtonHostComponent);
    fixture.detectChanges();
  });

  it('renders an accessible square button', () => {
    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
    button.click();
    expect(button.getAttribute('aria-label')).toBe('Close');
    expect(fixture.componentInstance.count).toBe(1);
  });
});

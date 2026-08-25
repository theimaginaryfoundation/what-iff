import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { ChatIconComponent } from './icons';

@Component({
  standalone: true,
  imports: [ChatIconComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: '<ui-chat-icon [size]="24" />',
})
class IconHostComponent {}

describe('icon components', () => {
  let fixture: ComponentFixture<IconHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [IconHostComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();

    fixture = TestBed.createComponent(IconHostComponent);
    fixture.detectChanges();
  });

  it('renders an accessible decorative svg at the requested size', () => {
    const svg = fixture.nativeElement.querySelector('svg') as SVGElement;

    expect(svg).toBeTruthy();
    expect(svg.getAttribute('width')).toBe('24');
    expect(svg.getAttribute('height')).toBe('24');
    expect(svg.getAttribute('aria-hidden')).toBe('true');
    expect(svg.getAttribute('focusable')).toBe('false');
  });
});

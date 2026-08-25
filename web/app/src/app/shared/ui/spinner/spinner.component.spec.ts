import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { SpinnerComponent } from './spinner.component';

@Component({ standalone: true, imports: [SpinnerComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-spinner [size]="20" ariaLabel="Working" />' })
class SpinnerHostComponent {}

describe('SpinnerComponent', () => {
  let fixture: ComponentFixture<SpinnerHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [SpinnerHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(SpinnerHostComponent);
    fixture.detectChanges();
  });

  it('renders a labelled status svg', () => {
    const svg = fixture.nativeElement.querySelector('svg') as SVGElement;
    expect(svg.getAttribute('role')).toBe('status');
    expect(svg.getAttribute('aria-label')).toBe('Working');
    expect(svg.getAttribute('width')).toBe('20');
  });
});

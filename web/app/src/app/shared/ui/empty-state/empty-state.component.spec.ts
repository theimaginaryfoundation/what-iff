import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { EmptyStateComponent } from './empty-state.component';

@Component({ standalone: true, imports: [EmptyStateComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-empty-state heading="Nothing here" body="Try again" [level]="3"><button>Act</button></ui-empty-state>' })
class EmptyStateHostComponent {}

describe('EmptyStateComponent', () => {
  let fixture: ComponentFixture<EmptyStateHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [EmptyStateHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(EmptyStateHostComponent);
    fixture.detectChanges();
  });

  it('renders configured heading level, body and action slot', () => {
    expect(fixture.nativeElement.querySelector('h3').textContent).toContain('Nothing here');
    expect(fixture.nativeElement.textContent).toContain('Try again');
    expect(fixture.nativeElement.querySelector('button').textContent).toContain('Act');
  });
});

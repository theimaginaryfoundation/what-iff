import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { BadgeComponent } from './badge.component';

@Component({ standalone: true, imports: [BadgeComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-badge intent="success">Active</ui-badge>' })
class BadgeHostComponent {}

describe('BadgeComponent', () => {
  let fixture: ComponentFixture<BadgeHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [BadgeHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(BadgeHostComponent);
    fixture.detectChanges();
  });

  it('renders projected badge text', () => {
    const badge = fixture.nativeElement.querySelector('span') as HTMLElement;
    expect(badge.textContent).toContain('Active');
    expect(badge.className).toContain('rounded-full');
  });
});

import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { CardComponent } from './card.component';

@Component({ standalone: true, imports: [CardComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-card variant="elevated"><div card-header>Header</div>Body<div card-footer>Footer</div></ui-card>' })
class CardHostComponent {}

describe('CardComponent', () => {
  let fixture: ComponentFixture<CardHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [CardHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(CardHostComponent);
    fixture.detectChanges();
  });

  it('projects header body and footer slots', () => {
    expect(fixture.nativeElement.textContent).toContain('Header');
    expect(fixture.nativeElement.textContent).toContain('Body');
    expect(fixture.nativeElement.textContent).toContain('Footer');
  });
});

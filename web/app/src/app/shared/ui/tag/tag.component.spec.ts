import { Component, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TagComponent } from './tag.component';

@Component({ standalone: true, imports: [TagComponent], changeDetection: ChangeDetectionStrategy.Eager,
 template: '<ui-tag [selected]="true" (toggle)="count = count + 1">Pinned</ui-tag>' })
class TagHostComponent { count = 0; }

describe('TagComponent', () => {
  let fixture: ComponentFixture<TagHostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [TagHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(TagHostComponent);
    fixture.detectChanges();
  });

  it('renders aria-pressed and emits toggle', () => {
    const button = fixture.nativeElement.querySelector('button') as HTMLButtonElement;
    expect(button.getAttribute('aria-pressed')).toBe('true');
    button.click();
    button.dispatchEvent(new KeyboardEvent('keydown', { key: ' ' }));
    expect(fixture.componentInstance.count).toBe(2);
  });
});

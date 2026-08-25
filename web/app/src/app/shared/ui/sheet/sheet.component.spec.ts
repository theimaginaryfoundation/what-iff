import { Component, signal, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { SheetComponent } from './sheet.component';
import { resetBodyScrollLockForTests } from '../helpers/body-scroll-lock.helpers';

@Component({
  standalone: true,
  imports: [SheetComponent],
  changeDetection: ChangeDetectionStrategy.Eager,
  template: '<ui-sheet [open]="open()" labelledBy="sheet-title" (dismiss)="dismissed = dismissed + 1"><h2 sheet-header id="sheet-title">Sheet</h2><button>Inside</button></ui-sheet>',
})
class SheetHostComponent {
  open = signal(true);
  dismissed = 0;
}

describe('SheetComponent', () => {
  let fixture: ComponentFixture<SheetHostComponent>;

  beforeEach(async () => {
    resetBodyScrollLockForTests();
    document.body.classList.remove('overflow-hidden');
    await TestBed.configureTestingModule({ imports: [SheetHostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(SheetHostComponent);
    fixture.detectChanges();
    await new Promise(resolve => setTimeout(resolve, 0));
  });

  afterEach(() => {
    document.body.classList.remove('overflow-hidden');
    resetBodyScrollLockForTests();
  });

  it('renders a bottom-sheet dialog and dismisses on backdrop click', () => {
    expect(fixture.nativeElement.querySelector('[role="dialog"]')).toBeTruthy();
    const backdrop = fixture.nativeElement.querySelector('.ui-sheet') as HTMLElement;
    backdrop.click();
    expect(fixture.componentInstance.dismissed).toBe(1);
  });
});

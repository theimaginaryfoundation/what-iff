import { Component, provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { OverlayComponent } from './overlay.component';

@Component({
  standalone: true,
  imports: [OverlayComponent],
  template: `
    <button type="button" id="outside">outside</button>
    <ui-overlay [open]="open()" labelledBy="t" (dismiss)="dismissed = dismissed + 1">
      <h2 id="t">Title</h2>
      <button type="button" id="inside">inside</button>
    </ui-overlay>
  `,
})
class HostComponent {
  readonly open = signal(false);
  dismissed = 0;
}

describe('OverlayComponent', () => {
  let fixture: ComponentFixture<HostComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({ imports: [HostComponent], providers: [provideZonelessChangeDetection()] }).compileComponents();
    fixture = TestBed.createComponent(HostComponent);
    fixture.detectChanges();
  });

  it('renders nothing while closed, then a labelled modal dialog that locks the page', async () => {
    expect(fixture.nativeElement.querySelector('[role="dialog"]')).toBeNull();
    fixture.componentInstance.open.set(true);
    fixture.detectChanges();
    await fixture.whenStable();
    const dialog = fixture.nativeElement.querySelector('[role="dialog"]') as HTMLElement;
    expect(dialog.getAttribute('aria-modal')).toBe('true');
    expect(dialog.getAttribute('aria-labelledby')).toBe('t');
    expect(document.body.classList.contains('overflow-hidden')).toBe(true);
    fixture.componentInstance.open.set(false);
    fixture.detectChanges();
    expect(document.body.classList.contains('overflow-hidden')).toBe(false);
  });

  it('emits dismiss on Escape', async () => {
    fixture.componentInstance.open.set(true);
    fixture.detectChanges();
    await fixture.whenStable();
    const dialog = fixture.nativeElement.querySelector('[role="dialog"]') as HTMLElement;
    dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(fixture.componentInstance.dismissed).toBe(1);
  });
});

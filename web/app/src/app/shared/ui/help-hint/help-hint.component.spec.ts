import { Component } from '@angular/core';
import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { HelpHintComponent } from './help-hint.component';

@Component({
  standalone: true,
  imports: [HelpHintComponent],
  template: `
    <button type="button" class="outside">outside</button>
    <ui-help-hint label="What is a personality?" heading="Personalities" [guide]="guide">
      A personality is who you're talking to.
    </ui-help-hint>
  `,
})
class HostComponent {
  guide: 'gettingStarted' | null = 'gettingStarted';
}

describe('HelpHintComponent', () => {
  let fixture: ComponentFixture<HostComponent>;
  let host: HTMLElement;
  const trigger = () => host.querySelector('.ui-help-hint__trigger') as HTMLButtonElement;
  const panel = () => host.querySelector('.ui-help-hint__panel');

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [HostComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(HostComponent);
    host = fixture.nativeElement as HTMLElement;
    fixture.detectChanges();
  });

  it('starts closed with an accessible, collapsed trigger', () => {
    expect(panel()).toBeNull();
    expect(trigger().getAttribute('aria-label')).toBe('What is a personality?');
    expect(trigger().getAttribute('aria-expanded')).toBe('false');
  });

  it('opens on click with the heading, body and guide link', () => {
    trigger().click();
    fixture.detectChanges();

    expect(trigger().getAttribute('aria-expanded')).toBe('true');
    expect(trigger().getAttribute('aria-controls')).toBe(panel()?.id);
    expect(panel()?.textContent).toContain('Personalities');
    expect(panel()?.textContent).toContain("A personality is who you're talking to.");
    const link = panel()?.querySelector('a') as HTMLAnchorElement;
    expect(link.href).toBe('https://whatiff.chat/guides/getting-started.html');
    expect(link.target).toBe('_blank');
    expect(link.rel).toContain('noopener');
  });

  it('closes on a second click, on Escape (returning focus), and on an outside click', () => {
    trigger().click();
    fixture.detectChanges();
    trigger().click();
    fixture.detectChanges();
    expect(panel()).toBeNull();

    trigger().click();
    fixture.detectChanges();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    fixture.detectChanges();
    expect(panel()).toBeNull();
    expect(document.activeElement).toBe(trigger());

    trigger().click();
    fixture.detectChanges();
    (host.querySelector('.outside') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(panel()).toBeNull();
  });

  it('keeps clicks inside the panel from closing it', () => {
    trigger().click();
    fixture.detectChanges();
    (panel() as HTMLElement).click();
    fixture.detectChanges();
    expect(panel()).not.toBeNull();
  });

  it('keeps the panel inside the viewport for a trigger near the right edge', () => {
    const width = window.innerWidth;
    vi.spyOn(trigger(), 'getBoundingClientRect').mockReturnValue(
      { left: width - 30, right: width - 2, top: 40, bottom: 68, width: 28, height: 28, x: width - 30, y: 40, toJSON: () => ({}) } as DOMRect,
    );
    trigger().click();
    fixture.detectChanges();

    const el = panel() as HTMLElement;
    const left = parseFloat(el.style.left);
    const panelWidth = parseFloat(el.style.width);
    expect(left).toBeGreaterThanOrEqual(16);
    expect(left + panelWidth).toBeLessThanOrEqual(width - 16);
    expect(parseFloat(el.style.top)).toBe(74);
  });

  it('closes when anything scrolls, since the panel is fixed-position', () => {
    trigger().click();
    fixture.detectChanges();
    document.body.dispatchEvent(new Event('scroll'));
    fixture.detectChanges();
    expect(panel()).toBeNull();
  });

  it('omits the guide link when no guide is given', () => {
    fixture.componentInstance.guide = null;
    fixture.changeDetectorRef.markForCheck();
    fixture.detectChanges();
    trigger().click();
    fixture.detectChanges();
    expect(panel()?.querySelector('a')).toBeNull();
  });
});

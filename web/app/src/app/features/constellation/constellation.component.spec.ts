import { TestBed } from '@angular/core/testing';
import { signal } from '@angular/core';
import { provideRouter } from '@angular/router';
import { AuthService } from '../../core/services/auth.service';
import { ConstellationComponent } from './constellation.component';
import { ConstellationService } from './constellation.service';

describe('ConstellationComponent', () => {
  beforeEach(async () => {
    localStorage.clear();
    await TestBed.configureTestingModule({
      imports: [ConstellationComponent],
      providers: [provideRouter([]), { provide: AuthService, useValue: { currentUser: signal({ id: 'learner' }) } }],
    }).compileComponents();
    TestBed.inject(ConstellationService).open();
  });
  afterEach(() => localStorage.clear());

  it('opens an objective from the map and exposes related named guide links plus the full catalog', async () => {
    const fixture = TestBed.createComponent(ConstellationComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    (fixture.nativeElement.querySelector('[data-objective="S1"]') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelector('#objective-title').textContent).toContain('Create Personality');
    const links = [...fixture.nativeElement.querySelectorAll('.objective__guides a')] as HTMLAnchorElement[];
    expect(links.some(link => link.href === 'https://whatiff.chat/guides/getting-started.html')).toBe(true);
    expect(links.every(link => link.target === '_blank' && link.rel.includes('noopener'))).toBe(true);
    fixture.componentInstance.guideLibraryOpen.set(true);
    fixture.detectChanges();
    expect(fixture.nativeElement.querySelectorAll('.guide-library a').length).toBe(19);
  });

  it('closes the selected objective before dismissing the overlay on Escape', async () => {
    const fixture = TestBed.createComponent(ConstellationComponent);
    fixture.detectChanges();
    fixture.componentInstance.select('S1');
    fixture.detectChanges();
    const shell = fixture.nativeElement.querySelector('.ui-overlay') as HTMLElement;
    shell.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(fixture.componentInstance.selectedId()).toBeNull();
    expect(TestBed.inject(ConstellationService).visible()).toBe(true);
    shell.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(TestBed.inject(ConstellationService).visible()).toBe(false);
  });
});

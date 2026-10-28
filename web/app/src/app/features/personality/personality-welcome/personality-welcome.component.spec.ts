import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { PersonalityWelcomeComponent } from './personality-welcome.component';

describe('PersonalityWelcomeComponent', () => {
  let fixture: ComponentFixture<PersonalityWelcomeComponent>;
  let host: HTMLElement;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [PersonalityWelcomeComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(PersonalityWelcomeComponent);
    host = fixture.nativeElement as HTMLElement;
    fixture.detectChanges();
  });

  it('explains the app before asking for a personality', () => {
    expect(host.querySelector('h2')?.textContent).toContain('What if your AI could grow with you?');
    expect(host.querySelectorAll('.welcome__highlight').length).toBe(3);
  });

  it('emits the chosen way to start', () => {
    const emitted: string[] = [];
    fixture.componentInstance.generate.subscribe(() => emitted.push('generate'));
    fixture.componentInstance.createManually.subscribe(() => emitted.push('create'));
    fixture.componentInstance.importData.subscribe(() => emitted.push('import'));

    (host.querySelector('.welcome__primary') as HTMLButtonElement).click();
    (host.querySelector('.welcome__secondary') as HTMLButtonElement).click();
    (host.querySelector('.welcome__inline-action') as HTMLButtonElement).click();

    expect(emitted).toEqual(['generate', 'create', 'import']);
  });

  it('offers mullet mode only when the server runs it', () => {
    expect(host.querySelector('[data-testid="welcome-mullet-mode"]')).toBeNull();

    fixture.componentRef.setInput('mulletModeAvailable', true);
    fixture.detectChanges();
    const offer = host.querySelector('[data-testid="welcome-mullet-mode"]') as HTMLElement;
    expect(offer.textContent).toContain('whatiff.chat');
    let opened = false;
    fixture.componentInstance.openMulletMode.subscribe(() => (opened = true));
    (offer.querySelector('button') as HTMLButtonElement).click();
    expect(opened).toBe(true);
  });

  it('links to the getting started guide in a new tab', () => {
    const link = host.querySelector('.welcome__guide') as HTMLAnchorElement;
    expect(link.href).toBe('https://whatiff.chat/guides/getting-started.html');
    expect(link.target).toBe('_blank');
    expect(link.rel).toContain('noopener');
  });
});

import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { SpeechAutoplayService } from '../../../../core/services/speech-autoplay.service';
import { SpeechPlayerService } from '../../../../core/services/speech-player.service';
import { SpeechService } from '../../../../core/services/speech.service';
import { AutoplayToggleComponent } from './autoplay-toggle.component';
import { of } from 'rxjs';

describe('AutoplayToggleComponent', () => {
  function render(hasVoice: boolean) {
    TestBed.configureTestingModule({
      imports: [AutoplayToggleComponent],
      providers: [
        provideZonelessChangeDetection(),
        { provide: SpeechService, useValue: { setChatAutoplay: vi.fn(() => of({})), pending: () => of([]) } },
      ],
    });
    TestBed.inject(SpeechPlayerService).setChatVoice('c1', hasVoice);
    const autoplay = TestBed.inject(SpeechAutoplayService);
    autoplay.setChatAutoplay('c1', { personality: true, override: null });
    const fixture = TestBed.createComponent(AutoplayToggleComponent);
    fixture.componentRef.setInput('chatId', 'c1');
    fixture.detectChanges();
    return { fixture, autoplay };
  }

  it('is hidden when the personality has no voice', () => {
    const { fixture } = render(false);
    expect(fixture.nativeElement.querySelector('[data-testid="autoplay-toggle"]')).toBeNull();
  });

  it("offers the personality's default, always and never, and saves the choice", async () => {
    const { fixture, autoplay } = render(true);
    const c = fixture.componentInstance;
    expect(c.on()).toBe(true);
    expect(c.choice()).toBe('default');
    expect(c.choices()[0].label).toBe('Personality default (on)');
    await c.pick('off');
    expect(autoplay.chatAutoplay('c1')).toEqual({ personality: true, override: false });
    expect(c.on()).toBe(false);
  });
});

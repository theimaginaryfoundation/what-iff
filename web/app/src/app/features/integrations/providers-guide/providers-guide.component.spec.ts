import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';

import { ProvidersGuideComponent } from './providers-guide.component';

describe('ProvidersGuideComponent', () => {
  let fixture: ComponentFixture<ProvidersGuideComponent>;

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [ProvidersGuideComponent],
      providers: [provideZonelessChangeDetection(), provideRouter([])],
    }).compileComponents();

    fixture = TestBed.createComponent(ProvidersGuideComponent);
    fixture.detectChanges();
  });

  it('marks OpenAI required and the rest optional', () => {
    const openai = fixture.componentInstance.providers.find(p => p.name === 'OpenAI');
    expect(openai?.requirement).toBe('Required');
    expect(fixture.componentInstance.providers.filter(p => p.requirement).length).toBe(1);
  });

  /**
   * The page exists to answer "why is OpenAI required when I chat with Claude",
   * so the answer has to be the specific list of things that run regardless of
   * the chat model — not a vague sentence.
   */
  it('lists the work OpenAI does regardless of the chat model', () => {
    const text = fixture.nativeElement.textContent;
    expect(text).toContain('Memory extraction and scratchpad updates');
    expect(text).toContain('Conversation summaries at each checkpoint');
    expect(text).toContain('Semantic search over your memories');
  });

  it('renders every provider with its uses', () => {
    const sections = fixture.nativeElement.querySelectorAll('section');
    expect(sections.length).toBe(fixture.componentInstance.providers.length);
    expect(fixture.nativeElement.textContent).toContain('Adds the Gemini models to your picker.');
  });
});

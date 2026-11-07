import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { AgentPromptQuestion, AgentPromptResponse } from '../../../../core/models/job.model';
import { AgentPromptQuestionsComponent } from './agent-prompt-questions.component';

const QUESTIONS: AgentPromptQuestion[] = [
  { id: 'q1', header: 'Format', question: 'Which format?', options: [{ label: 'PDF (Recommended)' }, { label: 'Markdown' }] },
  { id: 'q2', header: 'Sections', question: 'Which sections?', multi_select: true, options: [{ label: 'Summary' }, { label: 'Data' }] },
  { id: 'q3', header: 'Deadline', question: 'When do you need it?' },
  { id: 'q4', header: 'Notes', question: 'Anything else I should know?', optional: true },
];

describe('AgentPromptQuestionsComponent', () => {
  let fixture: ComponentFixture<AgentPromptQuestionsComponent>;
  let host: HTMLElement;
  let answers: AgentPromptResponse[];

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AgentPromptQuestionsComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(AgentPromptQuestionsComponent);
    host = fixture.nativeElement as HTMLElement;
    answers = [];
    fixture.componentInstance.answer.subscribe(a => answers.push(a));
    fixture.componentRef.setInput('promptId', 'p1');
    fixture.componentRef.setInput('questions', QUESTIONS);
    fixture.detectChanges();
  });

  const q = (sel: string) => host.querySelector(sel);
  const button = (testId: string) => q(`[data-testid=${testId}] button`) as HTMLButtonElement | null;
  const tabs = () => Array.from(host.querySelectorAll<HTMLButtonElement>('[role=tab]'));
  const update = () => fixture.detectChanges();

  function choose(label: string): void {
    const row = Array.from(host.querySelectorAll('app-prompt-choice')).find(el => el.textContent?.includes(label));
    (row?.querySelector('input') as HTMLInputElement).click();
    update();
  }

  function type(text: string): void {
    const box = q('textarea') as HTMLTextAreaElement;
    box.value = text;
    box.dispatchEvent(new Event('input'));
    update();
  }

  it('shows one question at a time, with a tab for each and where it is', () => {
    expect(tabs().map(t => t.textContent?.trim())).toEqual(['Format', 'Sections', 'Deadline', 'Notes']);
    expect(host.textContent).toContain('Question 1 of 4');
    expect(host.textContent).toContain('Which format?');
    expect(host.textContent).not.toContain('Which sections?');
    expect(host.textContent).toContain('Recommended');
    expect(button('agent-prompt-submit')).toBeNull();
    tabs()[2].click();
    update();
    expect(host.textContent).toContain('Question 3 of 4');
  });

  it('keeps the own-answer box closed until "Other" is picked', () => {
    expect(q('textarea')).toBeNull();
    choose('Other…');
    expect(q('textarea')).not.toBeNull();
    choose('Markdown');
    expect(q('textarea')).toBeNull();
  });

  it('goes back and forth, and only sends once every required question is answered', () => {
    choose('Markdown');
    button('agent-prompt-next')!.click();
    update();
    choose('Summary');
    choose('Data');
    button('agent-prompt-next')!.click();
    update();
    expect(q('textarea')).not.toBeNull(); // a required open question's box is simply there
    button('agent-prompt-next')!.click();
    update();

    // On the last question, which is optional: its box waits to be asked for.
    expect(host.textContent).toContain('Question 4 of 4');
    expect(q('textarea')).toBeNull();
    expect(button('agent-prompt-submit')!.disabled).toBe(true);
    expect(q('[data-testid=agent-prompt-remaining]')?.textContent).toContain('Deadline');

    button('agent-prompt-back')!.click();
    update();
    type('Friday');
    expect(button('agent-prompt-submit')!.disabled).toBe(false); // everything required is answered
    expect(tabs()[2].textContent).toContain('Answered');

    button('agent-prompt-next')!.click();
    update();
    choose('Add something…');
    type('  Keep it short ');
    button('agent-prompt-submit')!.click();
    expect(answers).toEqual([{
      action: 'accept',
      answers: { q1: { selected: ['Markdown'] }, q2: { selected: ['Summary', 'Data'] }, q3: { text: 'Friday' }, q4: { text: 'Keep it short' } },
    }]);
  });

  it('moves on by itself after a click on a first answer, but not after arrow keys', () => {
    vi.useFakeTimers();
    try {
      const row = host.querySelector('app-prompt-choice label') as HTMLElement;
      row.querySelector('input')!.click(); // as the arrow keys do: no press on the row
      update();
      vi.advanceTimersByTime(500);
      update();
      expect(host.textContent).toContain('Question 1 of 4');

      choose('Other…');
      fixture.componentInstance.go(0);
      fixture.componentInstance.drafts.update(d => ({ ...d, q1: { selected: [], text: '', other: false } }));
      update();
      const pdf = host.querySelector('app-prompt-choice label') as HTMLElement;
      pdf.dispatchEvent(new Event('pointerdown'));
      pdf.querySelector('input')!.click();
      update();
      vi.advanceTimersByTime(500);
      update();
      expect(host.textContent).toContain('Question 2 of 4');
    } finally {
      vi.useRealTimers();
    }
  });

  it('starts over for a new prompt', () => {
    choose('Markdown');
    button('agent-prompt-next')!.click();
    update();
    fixture.componentRef.setInput('promptId', 'p2');
    update();
    expect(host.textContent).toContain('Question 1 of 4');
    expect(tabs()[0].textContent).not.toContain('Answered');
  });
});

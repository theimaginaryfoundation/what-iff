import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { AgentPrompt, AgentPromptResponse } from '../../../../core/models/job.model';
import { AgentPromptCardComponent } from './agent-prompt-card.component';

describe('AgentPromptCardComponent', () => {
  let fixture: ComponentFixture<AgentPromptCardComponent>;
  let host: HTMLElement;
  let answers: AgentPromptResponse[];

  beforeEach(async () => {
    await TestBed.configureTestingModule({
      imports: [AgentPromptCardComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(AgentPromptCardComponent);
    host = fixture.nativeElement as HTMLElement;
    answers = [];
    fixture.componentInstance.answer.subscribe(a => answers.push(a));
  });

  function render(prompt: AgentPrompt): void {
    fixture.componentRef.setInput('prompt', prompt);
    fixture.detectChanges();
  }

  function click(el: Element | null | undefined): void {
    (el?.querySelector('button') ?? (el as HTMLElement))?.click();
    fixture.detectChanges();
  }

  it('approves with a note', () => {
    render({ id: 'p1', source: 'agent', expires_at: '2026-11-07T12:00:00Z', payload: { kind: 'approval', title: 'Delete the drafts', message: 'Three files' } });
    expect(host.textContent).toContain('Approval needed');
    expect(host.textContent).toContain('Delete the drafts');
    click(host.querySelector('[data-testid=agent-prompt-add-note]'));
    const note = host.querySelector('textarea') as HTMLTextAreaElement;
    note.value = '  keep the newest  ';
    note.dispatchEvent(new Event('input'));
    click(host.querySelector('[data-testid=agent-prompt-approve]'));
    expect(answers).toEqual([{ action: 'accept', note: 'keep the newest' }]);
  });

  it('hides the note until asked for', () => {
    render({ id: 'p1b', source: 'agent', expires_at: '2026-11-07T12:00:00Z', payload: { kind: 'approval', title: 'Delete the drafts' } });
    expect(host.querySelector('textarea')).toBeNull();
    click(host.querySelector('[data-testid=agent-prompt-deny]'));
    expect(answers).toEqual([{ action: 'decline', note: undefined }]);
  });

  it('sends a picked option; a single question has no tabs', () => {
    render({
      id: 'p2',
      source: 'agent',
      expires_at: '2026-11-07T12:00:00Z',
      payload: { kind: 'questions', questions: [{ id: 'q1', header: 'Format', question: 'Which format?', options: [{ label: 'PDF' }, { label: 'Markdown', description: 'Plain text' }] }] },
    });
    expect(host.querySelector('[role=tablist]')).toBeNull();
    const send = host.querySelector('[data-testid=agent-prompt-submit] button') as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    const radios = host.querySelectorAll<HTMLInputElement>('input[type=radio]');
    expect(radios).toHaveLength(3); // two options and "Other…"
    radios[1].click();
    fixture.detectChanges();
    expect(host.querySelector('textarea')).toBeNull();
    click(host.querySelector('[data-testid=agent-prompt-submit]'));
    expect(answers).toEqual([{ action: 'accept', answers: { q1: { selected: ['Markdown'] } }, note: undefined }]);
  });

  it('dismisses a questions prompt as declined, and an elicitation as cancelled', () => {
    render({ id: 'p4', source: 'agent', expires_at: '2026-11-07T12:00:00Z', payload: { kind: 'questions', questions: [{ id: 'q1', question: 'Why?' }] } });
    click(host.querySelector('[data-testid=agent-prompt-skip]'));
    render({ id: 'p5', source: 'mcp_elicitation', expires_at: '2026-11-07T12:00:00Z', payload: { kind: 'url', url: 'https://example.com/x' } });
    click(host.querySelector('[data-testid=agent-prompt-skip]'));
    expect(answers.map(a => a.action)).toEqual(['decline', 'cancel']);
  });

  it('names the connector and checks required form fields', () => {
    render({
      id: 'p3',
      source: 'mcp_elicitation',
      expires_at: '2026-11-07T12:00:00Z',
      payload: { kind: 'form', server_name: 'Test MCP', message: 'Who are you?', schema: { type: 'object', properties: { name: { type: 'string', title: 'Name' } }, required: ['name'] } },
    });
    expect(host.textContent).toContain('Test MCP is asking');
    click(host.querySelector('[data-testid=agent-prompt-submit]'));
    expect(answers).toEqual([]);
    expect(host.querySelector('[role=alert]')?.textContent).toContain('Name is required');
    const input = host.querySelector('input[type=text]') as HTMLInputElement;
    input.value = 'Gori';
    input.dispatchEvent(new Event('input'));
    click(host.querySelector('[data-testid=agent-prompt-submit]'));
    expect(answers[0]).toEqual({ action: 'accept', content: { name: 'Gori' }, note: undefined });
  });
});

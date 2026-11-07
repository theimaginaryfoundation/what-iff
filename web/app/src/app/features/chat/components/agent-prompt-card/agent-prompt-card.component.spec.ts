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
    const note = host.querySelector('textarea') as HTMLTextAreaElement;
    note.value = '  keep the newest  ';
    note.dispatchEvent(new Event('input'));
    click(host.querySelector('[data-testid=agent-prompt-approve]'));
    expect(answers).toEqual([{ action: 'accept', note: 'keep the newest' }]);
  });

  it('sends picked options and holds the send until something is answered', () => {
    render({
      id: 'p2',
      source: 'agent',
      expires_at: '2026-11-07T12:00:00Z',
      payload: { kind: 'questions', questions: [{ id: 'q1', header: 'Format', question: 'Which format?', options: [{ label: 'PDF' }, { label: 'Markdown', description: 'Plain text' }] }] },
    });
    const send = host.querySelector('[data-testid=agent-prompt-submit] button') as HTMLButtonElement;
    expect(send.disabled).toBe(true);
    const options = host.querySelectorAll<HTMLButtonElement>('.prompt__option');
    expect(options[1].getAttribute('role')).toBe('radio');
    options[1].click();
    fixture.detectChanges();
    expect(options[1].getAttribute('aria-checked')).toBe('true');
    click(host.querySelector('[data-testid=agent-prompt-submit]'));
    expect(answers).toEqual([{ action: 'accept', answers: { q1: { selected: ['Markdown'] } }, note: undefined }]);
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
    const input = host.querySelector('input') as HTMLInputElement;
    input.value = 'Gori';
    input.dispatchEvent(new Event('input'));
    click(host.querySelector('[data-testid=agent-prompt-submit]'));
    expect(answers[0]).toEqual({ action: 'accept', content: { name: 'Gori' }, note: undefined });
  });
});

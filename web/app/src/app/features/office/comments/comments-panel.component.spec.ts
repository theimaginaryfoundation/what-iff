import { provideZonelessChangeDetection } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';

import { CommentThread, newComment } from './comment-threads';
import { CommentsPanelComponent } from './comments-panel.component';

const ME = { id: 'user:u-1', name: 'Pat Lee' };
const SAM = { id: 'author:Sam Okafor', name: 'Sam Okafor' };
const DANA = { id: 'author:Dana Reyes', name: 'Dana Reyes' };

function threads(): CommentThread<null>[] {
  return [
    {
      id: 't1',
      anchor: null,
      quote: 'three features',
      comments: [
        newComment('t1', SAM, { text: 'Is three realistic, @Dana Reyes?', mentions: [DANA] }),
        newComment('c2', ME, { text: 'Yes', mentions: [] }),
      ],
    },
    { id: 't2', anchor: null, resolved: true, comments: [newComment('t2', DANA, { text: 'Done', mentions: [] })] },
  ];
}

describe('CommentsPanelComponent', () => {
  let fixture: ComponentFixture<CommentsPanelComponent>;
  let el: HTMLElement;

  async function render(inputs: Partial<{ readOnly: boolean; draftQuote: string | null; activeId: string | null }> = {}) {
    await TestBed.configureTestingModule({
      imports: [CommentsPanelComponent],
      providers: [provideZonelessChangeDetection()],
    }).compileComponents();
    fixture = TestBed.createComponent(CommentsPanelComponent);
    fixture.componentRef.setInput('threads', threads());
    fixture.componentRef.setInput('me', ME);
    fixture.componentRef.setInput('people', [ME, SAM, DANA]);
    for (const [key, value] of Object.entries(inputs)) fixture.componentRef.setInput(key, value);
    fixture.detectChanges();
    await fixture.whenStable();
    el = fixture.nativeElement as HTMLElement;
  }

  const box = (label: string) => el.querySelector<HTMLTextAreaElement>(`textarea[aria-label="${label}"]`)!;
  const type = (textarea: HTMLTextAreaElement, text: string) => {
    textarea.value = text;
    textarea.setSelectionRange(text.length, text.length);
    textarea.dispatchEvent(new Event('input'));
    fixture.detectChanges();
  };
  const key = (textarea: HTMLTextAreaElement, k: string) => {
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
    fixture.detectChanges();
  };

  it('lists open threads with their quote and mentions, and resolved ones on request', async () => {
    await render();
    const cards = () => [...el.querySelectorAll('.thread')].map(t => t.getAttribute('data-thread-id'));
    expect(cards()).toEqual(['t1']);
    expect(el.querySelector('.thread__quote')?.textContent).toBe('three features');
    expect(el.querySelector('.comment__mention')?.textContent).toBe('@Dana Reyes');
    (el.querySelector('.comments__filter') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(cards()).toEqual(['t1', 't2']);
  });

  it('offers only edit on your own comments', async () => {
    await render();
    const comments = [...el.querySelectorAll('.comment')];
    expect(comments[0].querySelector('[aria-label="Edit comment"]')).toBeNull();
    expect(comments[1].querySelector('[aria-label="Edit comment"]')).not.toBeNull();
  });

  it('read-only shows the threads without anything that would change them', async () => {
    await render({ readOnly: true });
    expect(el.querySelector('.comment__tools')).toBeNull();
    expect(el.querySelector('.thread__reply')).toBeNull();
  });

  it('a new thread: typing @ offers people, picking one writes the mention, Enter posts', async () => {
    await render({ draftQuote: 'the quote' });
    const created = vi.fn();
    fixture.componentInstance.create.subscribe(created);
    const textarea = box('New comment');
    type(textarea, 'Ask @da');
    const options = [...el.querySelectorAll('[role="option"]')].map(o => o.textContent?.trim());
    expect(options).toEqual(['Dana Reyes']);
    key(textarea, 'Enter');
    expect(textarea.value).toBe('Ask @Dana Reyes ');
    type(textarea, 'Ask @Dana Reyes about it');
    key(textarea, 'Enter');
    expect(created).toHaveBeenCalledWith({ text: 'Ask @Dana Reyes about it', mentions: [DANA] });
  });

  it('replies to a thread', async () => {
    await render({ activeId: 't1' });
    const replied = vi.fn();
    fixture.componentInstance.reply.subscribe(replied);
    (el.querySelector('.thread__reply') as HTMLButtonElement).click();
    fixture.detectChanges();
    const textarea = box('Reply');
    type(textarea, 'Sounds good');
    key(textarea, 'Enter');
    expect(replied).toHaveBeenCalledWith({ threadId: 't1', draft: { text: 'Sounds good', mentions: [] } });
  });
});

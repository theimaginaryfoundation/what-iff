import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { MARKED_EXTENSIONS, provideMarkdown } from 'ngx-markdown';
import { CommentPeopleService } from '../../office/comments/comment-people.service';
import { TextCommentsService } from '../text-comments/text-comments.service';
import { COMMENT_SAVER, CommentSaver } from '../editors/file-editor';
import { CommentsPanelState } from '../../office/comments/comments-panel-state.service';
import { fakeTextComments } from '../text-comments/fake-text-comments.spec-helper';

import { FileAttachment } from '../../../core/models/file-attachment.model';
import { latexMathExtension } from '../../../core/utils/latex-math.helpers';
import { fakeMonaco } from '../editors/fake-monaco.spec-helper';
import { MONACO_LOADER } from '../editors/monaco/monaco-loader';
import { TextViewerComponent } from './text-viewer.component';

function file(overrides: Partial<FileAttachment> = {}): FileAttachment {
  return { id: 'f-1', user_id: 'u-1', name: 'notes.txt', file_type: 'text/plain', created_at: '2026-05-01T00:00:00Z', ...overrides };
}

let textComments: ReturnType<typeof fakeTextComments>;
/** The workspace's comment saver: comments can be written once a test says the user may change the file. */
const saver = { canComment: signal(false), saveComments: vi.fn(), showCurrentContent: vi.fn() } satisfies CommentSaver;

describe('TextViewerComponent', () => {
  let fixture: ComponentFixture<TextViewerComponent>;
  let fake: ReturnType<typeof fakeMonaco>;

  beforeEach(async () => {
    textComments = fakeTextComments();
    saver.canComment.set(false);
    fake = fakeMonaco();
    await TestBed.configureTestingModule({
      imports: [TextViewerComponent],
      providers: [
        provideZonelessChangeDetection(),
        provideMarkdown({ markedExtensions: [{ provide: MARKED_EXTENSIONS, useFactory: latexMathExtension, multi: true }] }),
        { provide: MONACO_LOADER, useValue: () => Promise.resolve(fake.monaco) },
        { provide: CommentPeopleService, useValue: { me: signal({ id: 'user:u-1', name: 'Pat Lee' }) } },
        { provide: TextCommentsService, useValue: textComments },
        { provide: COMMENT_SAVER, useValue: saver },
      ],
    }).compileComponents();
    fixture = TestBed.createComponent(TextViewerComponent);
  });

  async function show(value: FileAttachment, text: string | Uint8Array): Promise<HTMLElement> {
    fixture.componentRef.setInput('file', value);
    fixture.componentRef.setInput('blob', new Blob([text as BlobPart]));
    fixture.detectChanges();
    await fixture.whenStable();
    await new Promise(resolve => setTimeout(resolve));
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture.nativeElement as HTMLElement;
  }

  const button = (el: HTMLElement, label: string) =>
    [...el.querySelectorAll('button')].find(b => b.textContent?.trim() === label) as HTMLButtonElement | undefined;

  it('shows a text file as text, never as markup', async () => {
    const el = await show(file({ name: 'page.html', file_type: 'text/html' }), '<b>bold?</b>\nline two');
    expect(el.querySelector('pre.text-viewer__text')?.textContent).toBe('<b>bold?</b>\nline two');
    expect(el.querySelector('b')).toBeNull();
    expect(fake.state.colorized).toEqual([]);
  });

  it('highlights code with a known language into static spans', async () => {
    const el = await show(file({ name: 'main.go', file_type: 'text/x-golang' }), 'package main');
    await vi.waitFor(() => expect(el.querySelector('pre.text-viewer__text--highlighted')).not.toBeNull());
    expect(fake.state.colorized).toEqual(['go']);
    expect(el.querySelector('pre.text-viewer__text span.mtk1')?.textContent).toBe('package main');
  });

  it('renders Markdown, with the raw text a click away', async () => {
    const el = await show(file({ name: 'notes.md', file_type: 'text/markdown' }), '# Title\n\nSome *text*.');
    expect(el.querySelector('markdown')).not.toBeNull();
    expect(el.querySelector('pre.text-viewer__text')).toBeNull();

    button(el, 'Raw')!.click();
    fixture.detectChanges();
    expect(el.querySelector('markdown')).toBeNull();
    expect(el.querySelector('pre.text-viewer__text')?.textContent).toBe('# Title\n\nSome *text*.');
  });

  it('renders LaTeX math in Markdown', async () => {
    const el = await show(file({ name: 'notes.md', file_type: 'text/markdown' }), 'Norm: $\\lVert x \\rVert$\n\n$$\nx_i^2\n$$');
    await vi.waitFor(() => expect(el.querySelectorAll('.katex')).toHaveLength(2));
    expect(el.querySelectorAll('.katex-display')).toHaveLength(1);
  });

  it('shows a CSV as a table with its header row', async () => {
    const el = await show(file({ name: 'scores.csv', file_type: 'text/csv' }), 'name,score\nAda,"9,5"\n');
    expect([...el.querySelectorAll('th')].map(th => th.textContent)).toEqual(['name', 'score']);
    expect([...el.querySelectorAll('td')].map(td => td.textContent)).toEqual(['Ada', '9,5']);
  });

  it('says so when a "text" file is really binary', async () => {
    const el = await show(file({ name: 'data.json', file_type: 'application/json' }), new Uint8Array([0x00, 0x01, 0x02, 0x00]));
    expect(el.textContent).toContain('does not look like text');
    expect(el.querySelector('pre.text-viewer__text')).toBeNull();
  });

  it('shows the newest bytes when the blob changes', async () => {
    await show(file(), 'first');
    const el = await show(file(), 'second');
    expect(el.querySelector('pre.text-viewer__text')?.textContent).toBe('second');
  });

  describe('comments', () => {
    const text = 'This is an inline comment in a text file';
    const txt = () =>
      ({ id: 'f-1', user_id: 'u', name: 'notes.txt', file_type: 'text/plain', created_at: '2026-11-01T00:00:00Z' }) as FileAttachment;

    it('shows the threads on the text, read-only without a saver', async () => {
      TestBed.inject(CommentsPanelState).open.set(null);
      textComments.state.stored = {
        contentVersion: 1,
        revision: 1,
        threads: [
          {
            id: 't',
            anchor: { start: 11, end: 25 },
            quote: 'inline comment',
            comments: [{ id: 't', authorId: 'author:Dana', authorName: 'Dana', createdAt: '2026-11-01T00:00:00Z', text: 'Hmm' }],
          },
        ],
      };
      const el = await show(txt(), text);
      await vi.waitFor(() => expect(fixture.componentInstance.comments.threads()).toHaveLength(1));
      fixture.detectChanges();
      expect(el.querySelector('app-comments-panel .thread__quote')?.textContent).toBe('inline comment');
      expect(fixture.componentInstance.canComment()).toBe(false);
      expect(el.querySelector('app-comments-panel .thread__reply')).toBeNull();
      expect(button(el, 'Comment')).toBeUndefined();
    });

    it('with a saver: comments on the selection and writes it straight away', async () => {
      saver.canComment.set(true);
      const el = await show(txt(), text);
      await vi.waitFor(() => expect(el.querySelector('pre.text-viewer__text')).not.toBeNull());
      const node = el.querySelector('pre.text-viewer__text')!.firstChild!;
      const range = document.createRange();
      range.setStart(node, 11);
      range.setEnd(node, 25);
      document.getSelection()!.removeAllRanges();
      document.getSelection()!.addRange(range);
      button(el, 'Comment')!.click();
      fixture.detectChanges();
      expect(el.querySelector('.thread--draft .thread__quote')?.textContent).toBe('inline comment');
      fixture.componentInstance.comments.create({ text: 'Rename?', mentions: [] });
      await vi.waitFor(() => expect(textComments.state.puts).toHaveLength(1));
      expect(textComments.state.puts[0]).toEqual(
        expect.objectContaining({ contentVersion: 1, threads: [expect.objectContaining({ anchor: { start: 11, end: 25 } })] }),
      );
    });
  });
});

import { provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { CommentMessage, CommentRef } from '../../../core/models/message.model';
import { FileWorkspaceService } from '../../file-workspace/file-workspace.service';
import { CommentAgentService, MentionQueue, NewReplies, agentPersonId } from './comment-agent.service';
import { CommentThread, newComment } from './comment-threads';

const ME = { id: 'user:u-1', name: 'Pat Lee' };
const SAM = { id: 'user:u-2', name: 'Sam' };
const IRIS = { id: agentPersonId('p-1'), name: 'Iris' };

describe('CommentAgentService', () => {
  let service: CommentAgentService;
  const workspaceChat = signal<string | null>('c-1');
  const sent: { to: string; text: string; comment: CommentMessage; ref: CommentRef }[] = [];

  beforeEach(() => {
    sent.length = 0;
    workspaceChat.set('c-1');
    TestBed.configureTestingModule({
      providers: [provideZonelessChangeDetection(), { provide: FileWorkspaceService, useValue: { chatId: workspaceChat } }],
    });
    service = TestBed.inject(CommentAgentService);
  });

  function register(chatId = 'c-1') {
    return service.register({
      chatId: signal(chatId),
      agent: signal(IRIS),
      sendComment: (to, text, comment, ref) => {
        sent.push({ to, text, comment, ref });
        return Promise.resolve(true);
      },
    });
  }

  it('offers the agent of the chat the file is open in, after me', () => {
    expect(service.withAgent([ME, SAM])).toEqual([ME, SAM]);
    const unregister = register();
    expect(service.withAgent([ME, SAM])).toEqual([ME, IRIS, SAM]);
    workspaceChat.set('c-2');
    expect(service.agent()).toBeNull();
    workspaceChat.set(null);
    expect(service.withAgent([ME])).toEqual([ME]);
    workspaceChat.set('c-1');
    unregister();
    expect(service.agent()).toBeNull();
  });

  it('sends a posted comment that mentions the agent, with the thread before it', async () => {
    register();
    const first = newComment('t1', SAM, { text: 'Which three?', mentions: [] });
    const reply = newComment('c2', ME, { text: '@Iris can you list them?', mentions: [{ id: IRIS.id, name: 'Iris' }] });
    const thread: CommentThread<null> = { id: 't1', anchor: null, comments: [first, reply] };
    const file = { id: 'f-1', name: 'plan.md' };

    expect(await service.posted(file, thread, 't1', 'major features')).toBe(false);
    expect(sent).toEqual([]);

    register();
    expect(await service.posted(file, thread, 'c2', 'major features')).toBe(true);
    expect(sent).toEqual([
      {
        to: 'c-1',
        text: '@Iris can you list them?',
        comment: {
          file_id: 'f-1',
          thread_id: 't1',
          comment_id: 'c2',
          quote: 'major features',
          earlier: [{ author_name: 'Sam', text: 'Which three?' }],
        },
        ref: { file_id: 'f-1', file_name: 'plan.md', thread_id: 't1', comment_id: 'c2', quote: 'major features' },
      },
    ]);
  });

  it('reloads an open file the agent commented on, once per reply', () => {
    const reload = vi.fn();
    const file = signal<{ id: string } | null>({ id: 'f-1' });
    TestBed.runInInjectionContext(() => service.watchAgentComments(file, reload));
    TestBed.tick();
    expect(reload).not.toHaveBeenCalled();
    service.agentCommented(['f-2']);
    TestBed.tick();
    expect(reload).not.toHaveBeenCalled();
    service.agentCommented(['f-1', 'f-2']);
    TestBed.tick();
    expect(reload).toHaveBeenCalledTimes(1);
    service.agentCommented([]);
    TestBed.tick();
    expect(reload).toHaveBeenCalledTimes(1);
  });
});

describe('NewReplies', () => {
  const reply = (id: string, sent_at: string, ...files: string[]) => ({
    id,
    origin: 'Assistant',
    sent_at,
    comment_refs: files.map(file_id => ({ file_id })),
  });

  it('reloads nothing for the replies a chat opens with, or older ones loaded later', () => {
    const replies = new NewReplies();
    expect(replies.filesCommentedOn([reply('a1', '2026-11-12T10:00:00Z', 'f1')])).toEqual([]);
    expect(replies.filesCommentedOn([reply('a0', '2026-11-11T10:00:00Z', 'f0'), reply('a1', '2026-11-12T10:00:00Z', 'f1')])).toEqual([]);
  });

  it('names the files only the new replies commented on, once each, when their refs arrive', () => {
    const replies = new NewReplies();
    replies.filesCommentedOn([reply('a1', '2026-11-12T10:00:00Z', 'f1')]);
    const user = { id: 'u1', origin: 'User', sent_at: '2026-11-12T11:00:00Z', comment_refs: [{ file_id: 'f9' }] };
    // The reply shows while it is written, without refs, then with them.
    expect(replies.filesCommentedOn([user, reply('a2', '2026-11-12T11:01:00Z')])).toEqual([]);
    expect(replies.filesCommentedOn([user, reply('a2', '2026-11-12T11:01:00Z', 'f2', 'f3')])).toEqual(['f2', 'f3']);
    expect(replies.filesCommentedOn([user, reply('a2', '2026-11-12T11:01:00Z', 'f2', 'f3')])).toEqual([]);
  });

  it('counts the first reply of a chat that had none, and starts over for another chat', () => {
    const replies = new NewReplies();
    expect(replies.filesCommentedOn([])).toEqual([]);
    expect(replies.filesCommentedOn([reply('a1', '2026-11-12T10:00:00Z', 'f1')])).toEqual(['f1']);
    replies.reset();
    expect(replies.filesCommentedOn([reply('b1', '2026-11-12T12:00:00Z', 'f5')])).toEqual([]); // the other chat opens with it
  });
});

describe('MentionQueue', () => {
  it('sends the mentions a landed save held, keeps later ones, and drops them on discard', () => {
    const sent: string[] = [];
    const q = new MentionQueue();
    q.add(() => sent.push('a'));
    q.add(() => sent.push('b'));
    const first = q.mark();
    q.add(() => sent.push('c')); // posted while that save ran
    q.saved(first);
    expect(sent).toEqual(['a', 'b']);
    q.saved(first);
    expect(sent).toEqual(['a', 'b']);

    // A save that failed sends nothing; the next one that lands sends what it held.
    const failed = q.mark();
    q.add(() => sent.push('d'));
    void failed;
    q.saved(q.mark());
    expect(sent).toEqual(['a', 'b', 'c', 'd']);

    q.add(() => sent.push('e'));
    const lost = q.mark();
    q.discard();
    q.saved(lost);
    expect(sent).toEqual(['a', 'b', 'c', 'd']);
  });
});

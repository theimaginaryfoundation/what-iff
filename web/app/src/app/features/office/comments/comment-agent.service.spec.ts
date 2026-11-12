import { provideZonelessChangeDetection, signal } from '@angular/core';
import { TestBed } from '@angular/core/testing';

import { CommentMessage, CommentRef } from '../../../core/models/message.model';
import { FileWorkspaceService } from '../../file-workspace/file-workspace.service';
import { CommentAgentService, agentPersonId } from './comment-agent.service';
import { CommentThread, newComment } from './comment-threads';

const ME = { id: 'user:u-1', name: 'Pat Lee' };
const SAM = { id: 'user:u-2', name: 'Sam' };
const IRIS = { id: agentPersonId('p-1'), name: 'Iris' };

describe('CommentAgentService', () => {
  let service: CommentAgentService;
  const workspaceChat = signal<string | null>('c-1');
  const sent: { text: string; comment: CommentMessage; ref: CommentRef }[] = [];

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
      sendComment: (text, comment, ref) => {
        sent.push({ text, comment, ref });
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

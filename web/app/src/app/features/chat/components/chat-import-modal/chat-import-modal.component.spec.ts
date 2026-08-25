import { Component, provideZonelessChangeDetection, signal, ChangeDetectionStrategy } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { By } from '@angular/platform-browser';
import { Observable, defer, of } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { PaginatedResponse } from '../../../../core/models/common.model';
import { Job, JobStatus } from '../../../../core/models/job.model';
import { ChatService } from '../../../../core/services/chat.service';
import { JobService } from '../../../../core/services/job.service';
import { ChatImportModalComponent } from './chat-import-modal.component';
import { ConversationImportChunkService } from './conversation-import-chunk.service';

const JOB_ID = '4da9a6fa-96f7-4e4b-8a83-760be6c486a3';
const IMPORTED_IDS = ['98e8ebba-12cb-415f-bc51-45e6619fc2ea', '405953cf-e067-43cc-b3dc-bf687227d463'];

/** The parsing-phase payload the backend writes at job creation: real counters, all zero. */
const PARSING_PROGRESS = '{"phase":"parsing","source":"openai","total":0,"imported":0,"skipped":0}';
/** A terminal payload copied from a real chat_import job row. */
const COMPLETE_PROGRESS = JSON.stringify({
    phase: 'complete',
    source: 'openai',
    total: 115,
    imported: 115,
    skipped: 0,
    imported_ids: IMPORTED_IDS,
});

function jobRow(status: JobStatus, progress: string): Job {
    return {
        id: JOB_ID,
        user_id: '020700fd-a594-4d5b-b89b-4f50c6fb3fe8',
        job_type: 'chat_import',
        reference: '020700fd-a594-4d5b-b89b-4f50c6fb3fe8',
        status,
        progress,
        created_at: '2026-07-28T22:08:26Z',
        updated_at: '2026-07-28T22:08:27Z',
    };
}

class StubJobService {
    /** Polled in order; the last entry repeats once exhausted. */
    responses: Job[] = [];
    calls = 0;

    getJob(): Observable<Job> {
        return defer(() => {
            const next = this.responses[Math.min(this.calls, this.responses.length - 1)];
            this.calls++;
            return of(next);
        });
    }
}

class StubChatService {
    listChatsPageCalls: string[] = [];

    importConversations(): Observable<Job> {
        return defer(() => of(jobRow('pending', PARSING_PROGRESS)));
    }

    listChatsPage(_page: number, _limit: number, filters?: {
        ids?: string;
    }): Observable<PaginatedResponse<Chat>> {
        this.listChatsPageCalls.push(filters?.ids ?? '');
        const results = IMPORTED_IDS.map(id => ({ id, name: `Imported ${id.slice(0, 4)}` }) as Chat);
        return of({ results, total_count: results.length, page: 1 });
    }
}

class StubChunkService {
    async splitExport(blob: Blob): Promise<{
        chunks: Blob[];
        totalConversations: number;
    }> {
        return { chunks: [blob], totalConversations: 115 };
    }
}

@Component({
    standalone: true,
    imports: [ChatImportModalComponent],
    changeDetection: ChangeDetectionStrategy.Eager,
    template: `<app-chat-import-modal [open]="open()" (imported)="importedEvents = importedEvents + 1" />`,
})
class HostComponent {
    readonly open = signal(true);
    importedEvents = 0;
}

describe('ChatImportModalComponent', () => {
    let fixture: ComponentFixture<HostComponent>;
    let modal: ChatImportModalComponent;
    let jobs: StubJobService;
    let chats: StubChatService;

    beforeEach(async () => {
        jobs = new StubJobService();
        chats = new StubChatService();

        await TestBed.configureTestingModule({
            imports: [HostComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: JobService, useValue: jobs },
                { provide: ChatService, useValue: chats },
                { provide: ConversationImportChunkService, useClass: StubChunkService },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(HostComponent);
        await fixture.whenStable();
        modal = fixture.debugElement.query(By.directive(ChatImportModalComponent)).componentInstance;
    });

    /** Selects a conversations.json so the modal reaches the 'ready' stage. */
    async function chooseExport(): Promise<void> {
        const file = new File(['[]'], 'conversations.json', { type: 'application/json' });
        const input = { files: [file], value: '' } as unknown as HTMLInputElement;
        await modal.onFileSelected({ target: input } as unknown as Event);
    }

    /** Runs the import and waits for a terminal stage (the poll loop uses real 1.5s timers). */
    async function runImport(timeoutMs = 6000): Promise<void> {
        modal.startImport();
        const deadline = Date.now() + timeoutMs;
        while (Date.now() < deadline) {
            const stage = modal.stage();
            if (stage === 'picker' || stage === 'done' || stage === 'error')
                break;
            await new Promise(resolve => setTimeout(resolve, 25));
        }
        await fixture.whenStable();
        fixture.detectChanges();
    }

    it('keeps the terminal counts when the job is still parsing on the first poll', async () => {
        jobs.responses = [
            jobRow('processing', PARSING_PROGRESS),
            jobRow('complete', COMPLETE_PROGRESS),
        ];

        await chooseExport();
        expect(modal.stage()).toBe('ready');

        await runImport();

        expect(jobs.calls).toBeGreaterThan(1);
        expect(modal.progress()?.imported).toBe(115);
        expect(modal.stage()).toBe('picker');
        expect(chats.listChatsPageCalls).toEqual([IMPORTED_IDS.join(',')]);
    });

    it('reports the dedup summary when every conversation was already imported', async () => {
        jobs.responses = [
            jobRow('complete', JSON.stringify({ phase: 'complete', source: 'openai', total: 115, imported: 0, skipped: 115 })),
        ];

        await chooseExport();
        await runImport();

        expect(modal.stage()).toBe('done');
        expect(modal.progress()?.skipped).toBe(115);
        expect(fixture.nativeElement.textContent).toContain('No new threads');
    });

    it('notifies the parent once so the archive list refreshes', async () => {
        jobs.responses = [jobRow('complete', COMPLETE_PROGRESS)];

        await chooseExport();
        await runImport();

        expect(fixture.componentInstance.importedEvents).toBe(1);
    });
});

import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { ActivatedRoute, convertToParamMap, Router } from '@angular/router';
import { BehaviorSubject, EMPTY, of } from 'rxjs';
import { provideMarkdown } from 'ngx-markdown';

import { ChatPageComponent } from './chat-page.component';
import { ChatService } from '../../core/services/chat.service';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ChatStreamingService } from '../../core/services/chat-streaming.service';
import { DraftMessageService } from '../../core/services/draft-message.service';
import { JobService } from '../../core/services/job.service';
import { MessageService } from '../../core/services/message.service';
import { ModelService } from '../../core/services/model.service';
import { PersonalityService } from '../../core/services/personality.service';
import { Chat } from '../../core/models/chat.model';
import { FileAttachment } from '../../core/models/file-attachment.model';
import { RightPanelService } from '../../core/services/right-panel.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { ContextPanelService } from './services/context-panel.service';
import { ChatSendGate, NoopChatSendGate } from './services/chat-send-gate';

describe('ChatPageComponent', () => {
    let fixture: ComponentFixture<ChatPageComponent>;
    let params$: BehaviorSubject<ReturnType<typeof convertToParamMap>>;
    let queryParams$: BehaviorSubject<ReturnType<typeof convertToParamMap>>;
    type ChatServiceMock = Pick<MockedObject<ChatService>, 'createChat' | 'createWelcomeMessage' | 'getChat' | 'getChatContext' | 'getLastChatId' | 'listChats' | 'patchChat' | 'setLastChatId' | 'clearLastChatId' | 'exportChat' | 'markChatRead' | 'listAllChats'>;
    type ImageGalleryServiceMock = Pick<MockedObject<ImageGalleryService>, 'referenceImage' | 'getImageUrl'>;
    type FileAttachmentServiceMock = Pick<MockedObject<FileAttachmentService>, 'uploadChatFileAttachment'>;
    type MessageServiceMock = Pick<MockedObject<MessageService>, 'clearMessages' | 'listMessages' | 'listBookmarks' | 'sendMessage' | 'setCurrentChatId' | 'markAssistantMessagesRead' | 'messages$'>;
    type RouterMock = Pick<MockedObject<Router>, 'navigate' | 'events'>;

    let chatService: ChatServiceMock;
    let imageGallery: ImageGalleryServiceMock;
    let fileAttachmentService: FileAttachmentServiceMock;
    let messageService: MessageServiceMock;
    let router: RouterMock;

    const chat: Chat = {
        id: 'chat-1',
        user_id: 'user-1',
        name: 'Test Chat',
        created_at: '',
        updated_at: '',
    };

    beforeEach(async () => {
        params$ = new BehaviorSubject(convertToParamMap({ id: 'chat-1' }));
        queryParams$ = new BehaviorSubject(convertToParamMap({}));
        chatService = {
            createChat: vi.fn().mockName("ChatService.createChat"),
            createWelcomeMessage: vi.fn().mockName("ChatService.createWelcomeMessage"),
            getChat: vi.fn().mockName("ChatService.getChat"),
            getChatContext: vi.fn().mockName("ChatService.getChatContext"),
            getLastChatId: vi.fn().mockName("ChatService.getLastChatId"),
            listChats: vi.fn().mockName("ChatService.listChats"),
            patchChat: vi.fn().mockName("ChatService.patchChat"),
            setLastChatId: vi.fn().mockName("ChatService.setLastChatId"),
            clearLastChatId: vi.fn().mockName("ChatService.clearLastChatId"),
            exportChat: vi.fn().mockName("ChatService.exportChat"),
            markChatRead: vi.fn().mockName("ChatService.markChatRead"),
            listAllChats: vi.fn().mockName("ChatService.listAllChats")
        } as unknown as ChatServiceMock;
        messageService = {
            clearMessages: vi.fn().mockName("MessageService.clearMessages"),
            listMessages: vi.fn().mockName("MessageService.listMessages"),
            listBookmarks: vi.fn().mockName("MessageService.listBookmarks"),
            sendMessage: vi.fn().mockName("MessageService.sendMessage"),
            setCurrentChatId: vi.fn().mockName("MessageService.setCurrentChatId"),
            markAssistantMessagesRead: vi.fn().mockName("MessageService.markAssistantMessagesRead"),
            messages$: new BehaviorSubject([]).asObservable()
        } as unknown as MessageServiceMock;
        const streamingService = {
            appendServerChunks: vi.fn().mockName("ChatStreamingService.appendServerChunks"),
            clearMessageState: vi.fn().mockName("ChatStreamingService.clearMessageState"),
            completeStreaming: vi.fn().mockName("ChatStreamingService.completeStreaming"),
            configure: vi.fn().mockName("ChatStreamingService.configure"),
            destroy: vi.fn().mockName("ChatStreamingService.destroy"),
            getDisplayMessage: vi.fn().mockName("ChatStreamingService.getDisplayMessage"),
            getDisplayRevision: vi.fn().mockName("ChatStreamingService.getDisplayRevision"),
            setCompletionCallback: vi.fn().mockName("ChatStreamingService.setCompletionCallback"),
            startStreaming: vi.fn().mockName("ChatStreamingService.startStreaming"),
            stopStreaming: vi.fn().mockName("ChatStreamingService.stopStreaming")
        };
        const draftService = {
            clearDraft: vi.fn().mockName("DraftMessageService.clearDraft"),
            getDraft: vi.fn().mockName("DraftMessageService.getDraft"),
            saveDraft: vi.fn().mockName("DraftMessageService.saveDraft")
        };
        const jobService = {
            pollJob: vi.fn().mockName("JobService.pollJob"),
            isJobBeingPolled: vi.fn().mockName("JobService.isJobBeingPolled")
        };
        const modelService = {
            getModels: vi.fn().mockName("ModelService.getModels"),
            models$: new BehaviorSubject([]).asObservable()
        };
        const personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        };
        personalityService.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        fileAttachmentService = {
            uploadChatFileAttachment: vi.fn().mockName("FileAttachmentService.uploadChatFileAttachment")
        } as unknown as FileAttachmentServiceMock;
        imageGallery = {
            referenceImage: vi.fn().mockName("ImageGalleryService.referenceImage"),
            getImageUrl: vi.fn().mockName("ImageGalleryService.getImageUrl")
        } as unknown as ImageGalleryServiceMock;
        router = {
            navigate: vi.fn().mockName("Router.navigate"),
            events: EMPTY
        } as unknown as RouterMock;

        chatService.getChat.mockReturnValue(of(chat));
        chatService.getChatContext.mockReturnValue(of({
            chat_id: 'chat-1',
            active_scratchpad: '',
            summary: 'Heaven\'s Gate recruited through escalating commitment.',
        }));
        chatService.createChat.mockReturnValue(of(chat));
        chatService.createWelcomeMessage.mockReturnValue(of({ id: 'msg-1', job_id: 'job-1', type: 'agent_job_run' }));
        chatService.patchChat.mockImplementation((_id: string, patch: {
            name?: string;
        }) => of({
            ...chat,
            ...(patch.name ? { name: patch.name } : {}),
        }));
        chatService.exportChat.mockReturnValue(of(void 0));
        chatService.listChats.mockReturnValue(of({ results: [chat], page: 1, total_count: 1 }));
        chatService.markChatRead.mockReturnValue(of({ updated_count: 0 }));
        chatService.listAllChats.mockReturnValue(of({ chats: [], truncated: false }));
        messageService.markAssistantMessagesRead.mockImplementation(() => {
        });
        messageService.listMessages.mockReturnValue(of({ results: [], page: 1, total_count: 0 }));
        messageService.listBookmarks.mockReturnValue(of([]));
        messageService.sendMessage.mockReturnValue(of({ id: 'msg-1', job_id: '', type: 'message' }));
        draftService.getDraft.mockReturnValue(null);
        fileAttachmentService.uploadChatFileAttachment.mockReturnValue(of(fileAttachment()));
        imageGallery.referenceImage.mockReturnValue(of(fileAttachment({ id: 'gallery-ref-1', name: 'fox.png' })));
        jobService.pollJob.mockReturnValue(of(null as any));
        jobService.isJobBeingPolled.mockReturnValue(false);
        modelService.getModels.mockReturnValue(of([]));
        streamingService.getDisplayMessage.mockImplementation(message => message.message);
        streamingService.getDisplayRevision.mockReturnValue(0);

        await TestBed.configureTestingModule({
            imports: [ChatPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                provideMarkdown(),
                {
                    provide: ActivatedRoute,
                    useValue: {
                        paramMap: params$.asObservable(),
                        queryParamMap: queryParams$.asObservable(),
                        snapshot: { paramMap: convertToParamMap({ id: 'chat-1' }) },
                    },
                },
                { provide: ImageGalleryService, useValue: imageGallery },
                { provide: Router, useValue: router },
                { provide: ChatService, useValue: chatService },
                { provide: FileAttachmentService, useValue: fileAttachmentService },
                { provide: MessageService, useValue: messageService },
                { provide: ChatStreamingService, useValue: streamingService },
                { provide: DraftMessageService, useValue: draftService },
                { provide: JobService, useValue: jobService },
                { provide: ModelService, useValue: modelService },
                { provide: PersonalityService, useValue: personalityService },
                { provide: ChatSendGate, useClass: NoopChatSendGate },
            ],
        }).compileComponents();

        localStorage.removeItem('contextPanel.tabsByChat.v1');
        TestBed.inject(ChatSendGate).clearCache();

        fixture = TestBed.createComponent(ChatPageComponent);

        // Root singletons are shared across the Karma run; other specs may leave the
        // panel open or persist tab choice for chat-1 in localStorage.
        TestBed.inject(RightPanelService).setVisible(false);
        TestBed.inject(ContextPanelService).closeMobile();
    });

    it('loads active chat from the route id', () => {
        fixture.detectChanges();

        expect(chatService.getChat).toHaveBeenCalledWith('chat-1');
        expect(fixture.nativeElement.textContent).toContain('Test Chat');
    });

    it('renders compact active chat shell and opens context rail tabs', async () => {
        const context = TestBed.inject(ContextPanelService);
        const rightPanel = TestBed.inject(RightPanelService);
        vi.spyOn(context, 'setActiveTab');

        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.chat-page__title-bar')).not.toBeNull();
        expect(fixture.nativeElement.querySelector('.chat-page__conversation-column')).not.toBeNull();
        expect(fixture.nativeElement.querySelector('app-chat-composer')).not.toBeNull();
        // Chat page opens the context panel on init; start from scratchpad so the
        // memories click selects a new tab instead of toggling the panel closed.
        expect(context.activeTab()).toBe('scratchpad');
        expect(rightPanel.visible()).toBe(true);

        const memoriesButton = fixture.nativeElement.querySelector('.chat-page__context-rail [aria-label="Open memories"]') as HTMLButtonElement;
        expect(memoriesButton, 'context rail memories button').toBeTruthy();

        memoriesButton.click();
        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();

        expect(context.setActiveTab).toHaveBeenCalledWith('memories');
        expect(rightPanel.visible()).toBe(true);
        expect(memoriesButton.classList).toContain('chat-page__context-rail-button--active');
        expect(memoriesButton.getAttribute('aria-current')).toBe('true');
    });

    it('exports the active thread from the compact title bar', () => {
        fixture.detectChanges();

        const exportButton = fixture.nativeElement.querySelector('.chat-page__export') as HTMLButtonElement;
        exportButton.click();

        expect(chatService.exportChat).toHaveBeenCalledWith('chat-1');
    });

    it('renames the active thread when title edit blurs with a dirty non-empty value', async () => {
        fixture.detectChanges();

        const trigger = fixture.nativeElement.querySelector('.chat-page__thread-name-trigger') as HTMLButtonElement;
        trigger.click();
        fixture.detectChanges();

        const input = fixture.nativeElement.querySelector('.chat-page__thread-name-input') as HTMLInputElement;
        input.value = 'Heaven\'s Gate Research';
        input.dispatchEvent(new Event('input'));
        input.dispatchEvent(new Event('blur'));
        fixture.detectChanges();
        await fixture.whenStable();

        expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { name: 'Heaven\'s Gate Research' });
    });

    it('does not rename when title edit blurs with empty value', () => {
        fixture.detectChanges();
        chatService.patchChat.mockClear();

        const trigger = fixture.nativeElement.querySelector('.chat-page__thread-name-trigger') as HTMLButtonElement;
        trigger.click();
        fixture.detectChanges();

        const input = fixture.nativeElement.querySelector('.chat-page__thread-name-input') as HTMLInputElement;
        input.value = '   ';
        input.dispatchEvent(new Event('input'));
        input.dispatchEvent(new Event('blur'));
        fixture.detectChanges();

        expect(chatService.patchChat).not.toHaveBeenCalled();
    });

    it('renders a summary banner and opens a summary modal on click', () => {
        fixture.detectChanges();

        const banner = fixture.nativeElement.querySelector('.chat-page__summary-bar') as HTMLButtonElement;
        expect(banner).not.toBeNull();
        expect(banner.textContent).toContain('Summary');

        banner.click();
        fixture.detectChanges();

        const modal = fixture.nativeElement.querySelector('.chat-page__summary-modal') as HTMLElement | null;
        expect(modal).not.toBeNull();
        expect(modal?.textContent).toContain('Heaven\'s Gate recruited through escalating commitment.');
    });

    it('closes the active thread and returns to the thread manager', async () => {
        fixture.detectChanges();

        const closeButton = fixture.nativeElement.querySelector('.chat-page__close') as HTMLButtonElement;
        closeButton.click();
        await fixture.whenStable();

        expect(chatService.clearLastChatId).toHaveBeenCalled();
        expect(router.navigate).toHaveBeenCalledWith(['/chat']);
    });

    it('references a gallery image into pending attachments from galleryImageId query param', async () => {
        queryParams$.next(convertToParamMap({ galleryImageId: 'gallery-img-1' }));
        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();

        expect(imageGallery.referenceImage).toHaveBeenCalledWith('gallery-img-1');
        expect(fixture.componentInstance.pendingAttachments()).toEqual([
            expect.objectContaining({
                attachment: expect.objectContaining({ id: 'gallery-ref-1' }),
                isUploading: false,
            }),
        ]);
        expect(router.navigate).toHaveBeenCalledWith([], expect.objectContaining({
            queryParams: { galleryImageId: null, welcome: null },
            queryParamsHandling: 'merge',
            replaceUrl: true,
        }));
    });

    it('triggers welcome job when welcome=true and chat has no messages', async () => {
        queryParams$.next(convertToParamMap({ welcome: 'true' }));
        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();

        expect(chatService.createWelcomeMessage).toHaveBeenCalledWith('chat-1');
        expect(router.navigate).toHaveBeenCalledWith([], expect.objectContaining({
            queryParams: { welcome: null },
            queryParamsHandling: 'merge',
            replaceUrl: true,
        }));
    });

    it('skips polling when welcome endpoint returns no-op', async () => {
        chatService.createWelcomeMessage.mockReturnValue(of(null));
        queryParams$.next(convertToParamMap({ welcome: 'true' }));
        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();

        expect(chatService.createWelcomeMessage).toHaveBeenCalledWith('chat-1');
        expect((TestBed.inject(JobService) as MockedObject<JobService>).pollJob).not.toHaveBeenCalled();
    });

    it('uploads selected composer files and sends them with the message', async () => {
        fixture.detectChanges();
        const file = new File(['hello'], 'notes.txt', { type: 'text/plain' });

        fixture.componentInstance.onFilesSelected([file]);
        await fixture.whenStable();
        fixture.detectChanges();

        expect(fileAttachmentService.uploadChatFileAttachment).toHaveBeenCalledWith('chat-1', file);
        expect(fixture.componentInstance.pendingAttachments()[0].attachment?.id).toBe('attachment-1');

        await fixture.componentInstance.send('Use this file');

        expect(messageService.sendMessage).toHaveBeenCalledWith('chat-1', expect.objectContaining({
            message: 'Use this file',
            attachments: [expect.objectContaining({ id: 'attachment-1' })],
        }));
        expect(fixture.componentInstance.pendingAttachments()).toEqual([]);
    });

    it('redirects bare /chat to the last active chat', () => {
        chatService.getLastChatId.mockReturnValue('last-chat');
        params$.next(convertToParamMap({}));

        fixture.detectChanges();

        expect(router.navigate).toHaveBeenCalledWith(['/chat', 'last-chat'], { replaceUrl: true });
    });

    it('renders thread manager in main content when no active thread', async () => {
        chatService.getLastChatId.mockReturnValue(null);
        params$.next(convertToParamMap({}));

        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('Thread Manager');
        expect(fixture.nativeElement.textContent).toContain('No threads found');
    });
});

function fileAttachment(overrides: Partial<FileAttachment> = {}): FileAttachment {
    return {
        id: 'attachment-1',
        user_id: 'user-1',
        name: 'notes.txt',
        file_type: 'text/plain',
        created_at: '',
        ...overrides,
    };
}

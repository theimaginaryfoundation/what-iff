import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { DraftMessageService } from './draft-message.service';
import { FileAttachment } from '../models/file-attachment.model';
import { Ritual } from '../models/ritual.model';

describe('DraftMessageService', () => {
    let service: DraftMessageService;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection()
            ]
        });
        service = TestBed.inject(DraftMessageService);
        // Clear localStorage before each test
        localStorage.clear();
    });

    afterEach(() => {
        // Clean up after each test
        localStorage.clear();
    });

    it('should be created', () => {
        expect(service).toBeTruthy();
    });

    describe('saveDraft', () => {
        it('should save a draft message to localStorage with per-chat key', () => {
            const chatId = 'chat-1';
            const message = 'Test message';

            service.saveDraft(chatId, message);

            const savedDraft = localStorage.getItem('draftMessage_chat-1');
            expect(savedDraft).toBeTruthy();

            const draft = JSON.parse(savedDraft!);
            expect(draft.chatId).toBe(chatId);
            expect(draft.message).toBe(message);
            expect(draft.timestamp).toBeDefined();
        });

        it('should save drafts for different chats separately', () => {
            service.saveDraft('chat-1', 'Message 1');
            service.saveDraft('chat-2', 'Message 2');

            const draft1 = localStorage.getItem('draftMessage_chat-1');
            const draft2 = localStorage.getItem('draftMessage_chat-2');

            expect(draft1).toBeTruthy();
            expect(draft2).toBeTruthy();
            expect(JSON.parse(draft1!).message).toBe('Message 1');
            expect(JSON.parse(draft2!).message).toBe('Message 2');
        });

        it('should save a draft with minimal attachment metadata only', () => {
            const chatId = 'chat-1';
            const message = 'Test message';
            const attachments: FileAttachment[] = [
                {
                    id: 'att-1',
                    user_id: 'user-1',
                    name: 'test.txt',
                    file_type: 'text/plain',
                    created_at: '2024-01-01T00:00:00Z'
                }
            ];

            service.saveDraft(chatId, message, attachments);

            const savedDraft = localStorage.getItem('draftMessage_chat-1');
            const draft = JSON.parse(savedDraft!);
            expect(draft.attachments).toBeDefined();
            expect(draft.attachments.length).toBe(1);
            // Only minimal metadata should be stored
            expect(draft.attachments[0]).toEqual({
                id: 'att-1',
                name: 'test.txt',
                file_type: 'text/plain'
            });
            // Full attachment fields should NOT be stored
            expect(draft.attachments[0].user_id).toBeUndefined();
            expect(draft.attachments[0].created_at).toBeUndefined();
        });

        it('should save a draft with minimal ritual metadata only', () => {
            const chatId = 'chat-1';
            const message = 'Test message';
            const rituals: Ritual[] = [
                {
                    id: 'ritual-1',
                    name: 'Test Ritual',
                    description: 'Test description',
                    content: 'Test content',
                    hotkeys: '',
                    personality_id: '',
                    created_at: '2024-01-01T00:00:00Z',
                    updated_at: '2024-01-01T00:00:00Z'
                }
            ];

            service.saveDraft(chatId, message, undefined, rituals);

            const savedDraft = localStorage.getItem('draftMessage_chat-1');
            const draft = JSON.parse(savedDraft!);
            expect(draft.rituals).toBeDefined();
            expect(draft.rituals.length).toBe(1);
            // Only minimal metadata should be stored
            expect(draft.rituals[0]).toEqual({
                id: 'ritual-1',
                name: 'Test Ritual'
            });
            // Full ritual fields should NOT be stored
            expect(draft.rituals[0].description).toBeUndefined();
            expect(draft.rituals[0].content).toBeUndefined();
            expect(draft.rituals[0].hotkeys).toBeUndefined();
        });

        it('should not save undefined attachments or rituals', () => {
            const chatId = 'chat-1';
            const message = 'Test message';

            service.saveDraft(chatId, message, [], []);

            const savedDraft = localStorage.getItem('draftMessage_chat-1');
            const draft = JSON.parse(savedDraft!);
            expect(draft.attachments).toBeUndefined();
            expect(draft.rituals).toBeUndefined();
        });
    });

    describe('getDraft', () => {
        it('should retrieve a draft for the correct chatId', () => {
            const chatId = 'chat-1';
            const message = 'Test message';
            service.saveDraft(chatId, message);

            const draft = service.getDraft(chatId);

            expect(draft).toBeTruthy();
            expect(draft!.chatId).toBe(chatId);
            expect(draft!.message).toBe(message);
        });

        it('should retrieve drafts for different chats independently', () => {
            service.saveDraft('chat-1', 'Message 1');
            service.saveDraft('chat-2', 'Message 2');

            const draft1 = service.getDraft('chat-1');
            const draft2 = service.getDraft('chat-2');

            expect(draft1!.message).toBe('Message 1');
            expect(draft2!.message).toBe('Message 2');
        });

        it('should return null for a chatId with no draft', () => {
            service.saveDraft('chat-1', 'Test message');

            const draft = service.getDraft('chat-2');

            expect(draft).toBeNull();
        });

        it('should return null if no draft exists', () => {
            const draft = service.getDraft('chat-1');

            expect(draft).toBeNull();
        });

        it('should return null and clear draft if older than 24 hours', () => {
            const chatId = 'chat-1';
            const message = 'Test message';

            // Save a draft with old timestamp
            const oldTimestamp = Date.now() - (25 * 60 * 60 * 1000); // 25 hours ago
            localStorage.setItem('draftMessage_chat-1', JSON.stringify({
                chatId,
                message,
                timestamp: oldTimestamp
            }));

            const draft = service.getDraft(chatId);

            expect(draft).toBeNull();
            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull();
        });

        it('should handle corrupted JSON gracefully', () => {
            localStorage.setItem('draftMessage_chat-1', 'invalid json {');
            vi.spyOn(console, 'error').mockReturnValue(undefined);

            const draft = service.getDraft('chat-1');

            expect(draft).toBeNull();
            expect(console.error).toHaveBeenCalledWith('Failed to parse draft message:', expect.any(Error));
            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull();
        });

        it('should retrieve draft with minimal attachment metadata', () => {
            const chatId = 'chat-1';
            const attachments: FileAttachment[] = [
                {
                    id: 'att-1',
                    user_id: 'user-1',
                    name: 'test.txt',
                    file_type: 'text/plain',
                    created_at: '2024-01-01T00:00:00Z'
                }
            ];
            service.saveDraft(chatId, 'Message', attachments);

            const draft = service.getDraft(chatId);

            expect(draft!.attachments).toBeDefined();
            expect(draft!.attachments!.length).toBe(1);
            // Only minimal metadata should be retrieved
            expect(draft!.attachments![0]).toEqual({
                id: 'att-1',
                name: 'test.txt',
                file_type: 'text/plain'
            });
        });

        it('should retrieve draft with minimal ritual metadata', () => {
            const chatId = 'chat-1';
            const rituals: Ritual[] = [
                {
                    id: 'ritual-1',
                    name: 'Test Ritual',
                    description: 'Test description',
                    content: 'Test content',
                    hotkeys: '',
                    personality_id: '',
                    created_at: '2024-01-01T00:00:00Z',
                    updated_at: '2024-01-01T00:00:00Z'
                }
            ];
            service.saveDraft(chatId, 'Message', undefined, rituals);

            const draft = service.getDraft(chatId);

            expect(draft!.rituals).toBeDefined();
            expect(draft!.rituals!.length).toBe(1);
            // Only minimal metadata should be retrieved
            expect(draft!.rituals![0]).toEqual({
                id: 'ritual-1',
                name: 'Test Ritual'
            });
        });

        it('should reject draft with invalid schema', () => {
            const chatId = 'chat-1';
            // Manually insert invalid data into localStorage
            localStorage.setItem('draftMessage_chat-1', JSON.stringify({
                chatId: 'chat-1',
                message: 'Test',
                timestamp: Date.now(),
                attachments: [
                    { id: 'att-1' } // Missing required name and file_type fields
                ]
            }));

            const draft = service.getDraft(chatId);

            expect(draft).toBeNull();
            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull(); // Should be cleared
        });

        it('should skip attachments without required fields', () => {
            const chatId = 'chat-1';
            const attachments: FileAttachment[] = [
                {
                    id: 'att-1',
                    user_id: 'user-1',
                    name: 'valid.txt',
                    file_type: 'text/plain',
                    created_at: '2024-01-01T00:00:00Z'
                },
                {
                    id: '', // Invalid: empty id
                    user_id: 'user-1',
                    name: 'invalid.txt',
                    file_type: 'text/plain',
                    created_at: '2024-01-01T00:00:00Z'
                } as any
            ];

            service.saveDraft(chatId, 'Message', attachments);
            const draft = service.getDraft(chatId);

            expect(draft!.attachments!.length).toBe(1); // Only valid attachment saved
            expect(draft!.attachments![0].id).toBe('att-1');
        });
    });

    describe('clearDraft', () => {
        it('should clear a specific chat draft from localStorage', () => {
            service.saveDraft('chat-1', 'Test message');
            expect(localStorage.getItem('draftMessage_chat-1')).toBeTruthy();

            service.clearDraft('chat-1');

            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull();
        });

        it('should only clear the specified chat draft, not others', () => {
            service.saveDraft('chat-1', 'Message 1');
            service.saveDraft('chat-2', 'Message 2');

            service.clearDraft('chat-1');

            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull();
            expect(localStorage.getItem('draftMessage_chat-2')).toBeTruthy();
        });

        it('should clear all drafts when no chatId is provided', () => {
            service.saveDraft('chat-1', 'Message 1');
            service.saveDraft('chat-2', 'Message 2');
            service.saveDraft('chat-3', 'Message 3');

            service.clearDraft();

            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull();
            expect(localStorage.getItem('draftMessage_chat-2')).toBeNull();
            expect(localStorage.getItem('draftMessage_chat-3')).toBeNull();
        });

        it('should not throw error if no draft exists for the chatId', () => {
            expect(() => service.clearDraft('non-existent-chat')).not.toThrow();
        });

        it('should not throw error if no drafts exist when clearing all', () => {
            expect(() => service.clearDraft()).not.toThrow();
        });

        it('should not clear other localStorage items when clearing all drafts', () => {
            localStorage.setItem('otherKey', 'other value');
            service.saveDraft('chat-1', 'Message 1');

            service.clearDraft();

            expect(localStorage.getItem('otherKey')).toBe('other value');
            expect(localStorage.getItem('draftMessage_chat-1')).toBeNull();
        });
    });

    describe('hasDraft', () => {
        it('should return true if a valid draft exists for the chatId', () => {
            const chatId = 'chat-1';
            service.saveDraft(chatId, 'Test message');

            expect(service.hasDraft(chatId)).toBe(true);
        });

        it('should return false if no draft exists for the chatId', () => {
            expect(service.hasDraft('chat-1')).toBe(false);
        });

        it('should return false if checking a different chat that has no draft', () => {
            service.saveDraft('chat-1', 'Test message');

            expect(service.hasDraft('chat-2')).toBe(false);
        });

        it('should return true only for chats with drafts', () => {
            service.saveDraft('chat-1', 'Message 1');
            service.saveDraft('chat-3', 'Message 3');

            expect(service.hasDraft('chat-1')).toBe(true);
            expect(service.hasDraft('chat-2')).toBe(false);
            expect(service.hasDraft('chat-3')).toBe(true);
        });

        it('should return false if draft is expired', () => {
            const chatId = 'chat-1';
            const oldTimestamp = Date.now() - (25 * 60 * 60 * 1000); // 25 hours ago
            localStorage.setItem('draftMessage_chat-1', JSON.stringify({
                chatId,
                message: 'Test',
                timestamp: oldTimestamp
            }));

            expect(service.hasDraft(chatId)).toBe(false);
        });
    });
});


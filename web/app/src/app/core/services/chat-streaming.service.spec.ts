import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { ChatStreamingService } from './chat-streaming.service';
import { ChatMessage } from '../models/message.model';

describe('ChatStreamingService', () => {
    let service: ChatStreamingService;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ChatStreamingService
            ]
        });
        service = TestBed.inject(ChatStreamingService);
    });

    afterEach(() => {
        service.destroy();
    });

    it('should be created', () => {
        expect(service).toBeTruthy();
    });

    describe('startStreaming', () => {
        it('should not stream empty messages', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: '',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            service.startStreaming(message);

            expect(service.getDisplayMessage(message)).toBe('');
        });

        it('should return full message for non-streaming messages', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hello world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            // Before streaming starts
            expect(service.getDisplayMessage(message)).toBe('Hello world');
        });
    });

    describe('visibility-aware chunk append', () => {
        it('flushes immediately when document is hidden', () => {
            const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden');
            let hidden = true;
            Object.defineProperty(document, 'hidden', {
                configurable: true,
                get: () => hidden,
            });
            const message: ChatMessage = {
                id: 'hidden-1',
                chat_id: 'chat1',
                message: '',
                origin: 'Assistant',
                sent_at: new Date().toISOString(),
            };

            service.appendServerChunks(message.id, ['Hello hidden world']);

            expect(service.getDisplayMessage(message)).toBe('Hello hidden world');
            expect(service.isStreaming(message.id, 'Hello hidden world')).toBe(false);

            if (hiddenDescriptor) {
                Object.defineProperty(document, 'hidden', hiddenDescriptor);
            }
        });

        it('flushes queued typing on visibilitychange', () => {
            const hiddenDescriptor = Object.getOwnPropertyDescriptor(document, 'hidden');
            let hidden = false;
            Object.defineProperty(document, 'hidden', {
                configurable: true,
                get: () => hidden,
            });
            service.configure({ intervalMs: 10000 });
            const message: ChatMessage = {
                id: 'hidden-2',
                chat_id: 'chat1',
                message: '',
                origin: 'Assistant',
                sent_at: new Date().toISOString(),
            };

            service.appendServerChunks(message.id, ['queued update']);
            expect(service.getDisplayMessage(message)).toBe('');

            hidden = true;
            document.dispatchEvent(new Event('visibilitychange'));

            expect(service.getDisplayMessage(message)).toBe('queued update');
            expect(service.isStreaming(message.id, 'queued update')).toBe(false);

            if (hiddenDescriptor) {
                Object.defineProperty(document, 'hidden', hiddenDescriptor);
            }
        });
    });

    describe('stopStreaming', () => {
        it('should set full text when stopped', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hello world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            // Stop streaming with full text
            service.stopStreaming(message.id, message.message);

            // Should display full message
            expect(service.getDisplayMessage(message)).toBe('Hello world');
        });
    });

    describe('stopAll', () => {
        it('should clear all streaming state', () => {
            const message1: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hello world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            const message2: ChatMessage = {
                id: '2',
                chat_id: 'chat1',
                message: 'Goodbye world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            // Start streaming both
            service.startStreaming(message1);
            service.startStreaming(message2);

            // Stop all
            service.stopAll();

            // Both should return original message (not streamed content)
            expect(service.getDisplayMessage(message1)).toBe('Hello world');
            expect(service.getDisplayMessage(message2)).toBe('Goodbye world');
        });
    });

    describe('isStreaming', () => {
        it('should return false when message is not streaming', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hello world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            expect(service.isStreaming(message.id, message.message)).toBe(false);
        });

        it('should return false when streaming is complete', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hi',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            // Stop with full text (simulates completion)
            service.stopStreaming(message.id, message.message);

            expect(service.isStreaming(message.id, message.message)).toBe(false);
        });
    });

    describe('getDisplayMessage', () => {
        it('should return full message when not streaming', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hello world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            expect(service.getDisplayMessage(message)).toBe('Hello world');
        });

        it('should preserve code blocks in message', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Here is code:\n```\nconst x = 1;\n```\nDone',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            // Stop with full text to ensure it's set
            service.stopStreaming(message.id, message.message);

            const result = service.getDisplayMessage(message);
            expect(result).toContain('```');
            expect(result).toContain('const x = 1;');
        });

        it('should preserve whitespace and newlines', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Line 1\nLine 2\n\nLine 3',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            // Stop with full text to ensure it's set
            service.stopStreaming(message.id, message.message);

            expect(service.getDisplayMessage(message)).toBe('Line 1\nLine 2\n\nLine 3');
        });
    });

    describe('setProgressCallback', () => {
        it('should allow setting a progress callback', () => {
            let callbackInvoked = false;
            service.setProgressCallback(() => {
                callbackInvoked = true;
            });

            // We can't easily test that it's called during streaming without fakeAsync,
            // but we can verify the method doesn't throw
            expect(callbackInvoked).toBe(false); // Not called yet
        });
    });

    describe('configure', () => {
        it('should allow custom configuration', () => {
            // Test that configure doesn't throw
            service.configure({
                intervalMs: 100,
                scrollCheckInterval: 10,
                codeBlockChunkSize: 100
            });

            // Configuration is applied internally, we trust it works
            expect(service).toBeTruthy();
        });

        it('should allow partial configuration', () => {
            // Test that partial config doesn't throw
            service.configure({ intervalMs: 50 });

            expect(service).toBeTruthy();
        });
    });

    describe('destroy', () => {
        it('should clean up all resources', () => {
            const message: ChatMessage = {
                id: '1',
                chat_id: 'chat1',
                message: 'Hello world',
                origin: 'Assistant',
                sent_at: new Date().toISOString()
            };

            service.startStreaming(message);
            service.destroy();

            // After destroy, messages should return original content
            expect(service.getDisplayMessage(message)).toBe('Hello world');
        });
    });
});

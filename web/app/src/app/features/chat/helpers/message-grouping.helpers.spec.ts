import { ChatMessage } from '../../../core/models/message.model';
import { appendPendingAssistantGroup, groupMessages, lastUserTurnWithGenerationError, needsPendingAssistantPlaceholder, pendingAssistantPlaceholderMessage, } from './message-grouping.helpers';
import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from '../chat.constants';

describe('groupMessages', () => {
    it('returns an empty list for no messages', () => {
        expect(groupMessages([])).toEqual([]);
    });

    it('clusters consecutive messages from the same author', () => {
        const groups = groupMessages([
            message('1', 'User'),
            message('2', 'User'),
            message('3', 'Assistant'),
        ]);

        expect(groups.length).toBe(2);
        expect(groups[0].kind).toBe('message-group');
        if (groups[0].kind === 'message-group') {
            expect(groups[0].messages.map(m => m.id)).toEqual(['1', '2']);
        }
    });

    it('splits assistant groups when generation personality changes', () => {
        const groups = groupMessages([
            message('1', 'Assistant', { generation_personality: 'Kai' }),
            message('2', 'Assistant', { generation_personality: 'Mira' }),
        ]);

        expect(groups.map(g => g.kind)).toEqual(['message-group', 'message-group']);
        expect(groups[0].kind).toBe('message-group');
        expect(groups[1].kind).toBe('message-group');
        if (groups[0].kind === 'message-group' && groups[1].kind === 'message-group') {
            expect(groups[0].messages.map(m => m.id)).toEqual(['1']);
            expect(groups[1].messages.map(m => m.id)).toEqual(['2']);
        }
    });

    it('splits assistant groups when generation expression changes', () => {
        const groups = groupMessages([
            message('1', 'Assistant', {
                generation_personality: 'Kai',
                generation_expression_key: 'happy',
                generation_expression_image_url: '/api/image-gallery/happy?size=full',
            }),
            message('2', 'Assistant', {
                generation_personality: 'Kai',
                generation_expression_key: 'sad',
                generation_expression_image_url: '/api/image-gallery/sad?size=full',
            }),
        ]);

        expect(groups.map(g => g.kind)).toEqual(['message-group', 'message-group']);
        if (groups[0].kind === 'message-group' && groups[1].kind === 'message-group') {
            expect(groups[0].messages.map(m => m.id)).toEqual(['1']);
            expect(groups[1].messages.map(m => m.id)).toEqual(['2']);
        }
    });

    it('keeps system-like messages as standalone items', () => {
        const groups = groupMessages([
            message('1', 'User'),
            { ...message('2', 'Assistant'), origin: 'System' as ChatMessage['origin'] },
            message('3', 'User'),
        ]);

        expect(groups.map(g => g.kind)).toEqual(['message-group', 'system-message', 'message-group']);
    });

    it('adds a model-change divider when assistant model changes', () => {
        const groups = groupMessages([
            message('1', 'Assistant', { generation_model: 'gpt-a' }),
            message('2', 'Assistant', { generation_model: 'gpt-b' }),
        ]);

        expect(groups.map(g => g.kind)).toEqual(['message-group', 'model-change-divider', 'message-group']);
        const divider = groups[1];
        expect(divider.kind).toBe('model-change-divider');
        if (divider.kind === 'model-change-divider') {
            expect(divider.previousModel).toBe('gpt-a');
            expect(divider.model).toBe('gpt-b');
        }
    });

    it('emits tool-call groups before the related message bubble', () => {
        const groups = groupMessages([
            message('1', 'Assistant', {
                tool_calls: [{
                        id: 'tool-1',
                        chat_message_id: '1',
                        tool_name: 'search',
                        tool_input: '{}',
                        tool_output: '{}',
                        tool_error: '',
                        created_at: '2024-01-01T00:00:00Z',
                        updated_at: '2024-01-01T00:00:00Z',
                    }],
            }),
        ]);

        expect(groups.map(g => g.kind)).toEqual(['tool-call-group', 'message-group']);
    });

    it('does not emit an empty assistant bubble when a message only contains tool calls', () => {
        const groups = groupMessages([
            message('1', 'Assistant', {
                message: '   ',
                tool_calls: [{
                        id: 'tool-1',
                        chat_message_id: '1',
                        tool_name: 'search',
                        tool_input: '{}',
                        tool_output: '{}',
                        tool_error: '',
                        created_at: '2024-01-01T00:00:00Z',
                        updated_at: '2024-01-01T00:00:00Z',
                    }],
            }),
        ]);

        expect(groups.map(g => g.kind)).toEqual(['tool-call-group']);
    });
});

describe('needsPendingAssistantPlaceholder', () => {
    it('is false when no assistant job is pending', () => {
        expect(needsPendingAssistantPlaceholder([message('1', 'User')], false)).toBe(false);
    });

    it('is true when a job is pending on an empty chat', () => {
        expect(needsPendingAssistantPlaceholder([], true)).toBe(true);
    });

    it('is true when a job is pending, user last, no assistant after', () => {
        expect(needsPendingAssistantPlaceholder([message('1', 'User')], true)).toBe(true);
    });

    it('is false once an assistant message exists after the last user', () => {
        expect(needsPendingAssistantPlaceholder([message('1', 'User'), message('2', 'Assistant')], true)).toBe(false);
    });
});

describe('appendPendingAssistantGroup', () => {
    it('appends a single-assistant message group', () => {
        const pending = pendingAssistantPlaceholderMessage({
            chatId: 'chat-1',
            draftText: 'Hello',
            generationPersonality: 'Kai',
            thinkingImageUrl: 'https://example.com/t.jpg',
        });
        expect(pending.id).toBe(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
        expect(pending.message).toBe('Hello');

        const extended = appendPendingAssistantGroup([], pending);
        expect(extended.length).toBe(1);
        expect(extended[0].kind).toBe('message-group');
        if (extended[0].kind === 'message-group') {
            expect(extended[0].messages).toEqual([pending]);
        }
    });
});

describe('lastUserTurnWithGenerationError', () => {
    it('returns the user message when it has an error and no assistant follows', () => {
        const u = message('u1', 'User', { last_error_message: 'boom' });
        expect(lastUserTurnWithGenerationError([u])).toBe(u);
    });

    it('returns null when an assistant replied after the user', () => {
        const u = message('u1', 'User', { last_error_message: 'boom' });
        const a = message('a1', 'Assistant');
        expect(lastUserTurnWithGenerationError([u, a])).toBeNull();
    });

    it('returns null without last_error_message', () => {
        expect(lastUserTurnWithGenerationError([message('u1', 'User')])).toBeNull();
    });
});

function message(id: string, origin: ChatMessage['origin'], overrides: Partial<ChatMessage> = {}): ChatMessage {
    return {
        id,
        chat_id: 'chat-1',
        message: `Message ${id}`,
        origin,
        sent_at: '2024-01-01T00:00:00Z',
        ...overrides,
    };
}

import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ChatService } from '../../../../../core/services/chat.service';
import { MCPServerService } from '../../../../../core/services/mcp-server.service';
import { RightPanelService } from '../../../../../core/services/right-panel.service';
import { ToolService } from '../../../../../core/services/tool.service';
import { ContextPanelService } from '../../../services/context-panel.service';
import { ContextToolsTabComponent } from './context-tools-tab.component';

describe('ContextToolsTabComponent', () => {
    let fixture: ComponentFixture<ContextToolsTabComponent>;

    beforeEach(async () => {
        const chatService = {
            patchChat: vi.fn().mockName("ChatService.patchChat")
        };
        chatService.patchChat.mockReturnValue(of({
            id: 'chat-1',
            user_id: 'user-1',
            name: 'Thread',
            disabled_tools: ['web_search'],
            created_at: '',
            updated_at: '',
        }));
        const toolService = {
            listTools: vi.fn().mockName("ToolService.listTools")
        };
        toolService.listTools.mockReturnValue(of([
            { name: 'web_search', description: 'Search the web for current information.' },
            { name: 'update_scratchpad', description: "Update this personality's working notes, which persist across conversations using the same personality." },
        ]));
        const mcpService = {
            listActiveForChat: vi.fn().mockName("MCPServerService.listActiveForChat"),
            listAvailableForChat: vi.fn().mockName("MCPServerService.listAvailableForChat"),
            addToChat: vi.fn().mockName("MCPServerService.addToChat"),
            removeFromChat: vi.fn().mockName("MCPServerService.removeFromChat")
        };
        mcpService.listActiveForChat.mockReturnValue(of([]));
        mcpService.listAvailableForChat.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        mcpService.addToChat.mockReturnValue(of(void 0));
        mcpService.removeFromChat.mockReturnValue(of(void 0));

        await TestBed.configureTestingModule({
            imports: [ContextToolsTabComponent],
            providers: [
                provideZonelessChangeDetection(),
                ContextPanelService,
                { provide: RightPanelService, useValue: {
                        setVisible: vi.fn().mockName("RightPanelService.setVisible")
                    } },
                { provide: ChatService, useValue: chatService },
                { provide: ToolService, useValue: toolService },
                { provide: MCPServerService, useValue: mcpService },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ContextToolsTabComponent);
        fixture.componentRef.setInput('chat', {
            id: 'chat-1',
            user_id: 'user-1',
            name: 'Thread',
            disabled_tools: [],
            created_at: '',
            updated_at: '',
        });
        fixture.componentRef.setInput('toolCalls', [{
                id: 'tool-call-1',
                chat_message_id: 'message-1',
                tool_name: 'web_search',
                tool_input: '{"query":"Austin vs Seattle cost of living 2026"}',
                tool_output: '',
                tool_error: '',
                created_at: '2026-04-04T13:03:00Z',
                updated_at: '2026-04-04T13:03:00Z',
            }]);
        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();
    });

    it('renders available tools with descriptions returned by the tools API', () => {
        expect(fixture.nativeElement.textContent).toContain('Available Tools');
        expect(fixture.nativeElement.textContent).toContain('Tool Call History');
        const labels = fixture.nativeElement.querySelectorAll('.tool-item');
        expect(labels.length).toBe(2);
        expect(fixture.nativeElement.textContent).toContain('Search the web for current information.');
        expect(fixture.nativeElement.textContent).toContain("Update this personality's working notes, which persist across conversations using the same personality.");
        expect(fixture.nativeElement.textContent).not.toContain('Austin vs Seattle cost of living 2026');
    });

    it('does not reload the tools list when only disabled_tools changes', async () => {
        const toolService = TestBed.inject(ToolService) as MockedObject<ToolService>;
        toolService.listTools.mockClear();

        fixture.componentRef.setInput('chat', {
            id: 'chat-1',
            user_id: 'user-1',
            name: 'Thread',
            disabled_tools: ['web_search'],
            created_at: '',
            updated_at: '',
        });
        fixture.detectChanges();
        await fixture.whenStable();

        expect(toolService.listTools).not.toHaveBeenCalled();
        expect(fixture.nativeElement.textContent).not.toContain('Loading tools…');
    });

    it('switches to loaded tool call history', () => {
        const historyTab = fixture.nativeElement.querySelectorAll('.context-tabs button')[1] as HTMLButtonElement;
        historyTab.click();
        fixture.detectChanges();

        expect(fixture.nativeElement.textContent).toContain('Web Search');
        expect(fixture.nativeElement.textContent).toContain('Search: "Austin vs Seattle cost of living 2026"');
        expect(fixture.nativeElement.querySelectorAll('.tool-item').length).toBe(0);
    });
});

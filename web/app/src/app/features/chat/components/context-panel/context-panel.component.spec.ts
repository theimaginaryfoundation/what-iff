import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';
import { Router } from '@angular/router';
import { of } from 'rxjs';

import { Chat } from '../../../../core/models/chat.model';
import { MemoryService } from '../../../../core/services/memory.service';
import { MCPServerService } from '../../../../core/services/mcp-server.service';
import { RightPanelService } from '../../../../core/services/right-panel.service';
import { ToolService } from '../../../../core/services/tool.service';
import { ContextPanelService } from '../../services/context-panel.service';
import { ScratchpadService } from '../../services/scratchpad.service';
import { ContextPanelComponent } from './context-panel.component';

describe('ContextPanelComponent', () => {
    let fixture: ComponentFixture<ContextPanelComponent>;
    let context: ContextPanelService;

    const chat: Chat = {
        id: 'chat-1',
        user_id: 'user-1',
        name: 'Thread',
        created_at: '',
        updated_at: '',
    };

    beforeEach(async () => {
        const memoryService = {
            getMemories: vi.fn().mockName("MemoryService.getMemories")
        };
        memoryService.getMemories.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));
        const toolService = {
            listTools: vi.fn().mockName("ToolService.listTools")
        };
        toolService.listTools.mockReturnValue(of([]));
        const mcpService = {
            listActiveForChat: vi.fn().mockName("MCPServerService.listActiveForChat"),
            listAvailableForChat: vi.fn().mockName("MCPServerService.listAvailableForChat")
        };
        mcpService.listActiveForChat.mockReturnValue(of([]));
        mcpService.listAvailableForChat.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        await TestBed.configureTestingModule({
            imports: [ContextPanelComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                ContextPanelService,
                ScratchpadService,
                { provide: Router, useValue: {
                        navigate: vi.fn().mockName("Router.navigate")
                    } },
                { provide: RightPanelService, useValue: {
                        setVisible: vi.fn().mockName("RightPanelService.setVisible")
                    } },
                { provide: MemoryService, useValue: memoryService },
                { provide: ToolService, useValue: toolService },
                { provide: MCPServerService, useValue: mcpService },
            ],
        }).compileComponents();

        context = TestBed.inject(ContextPanelService);
        context.setActiveChat(chat);
        fixture = TestBed.createComponent(ContextPanelComponent);
    });

    it('renders only the active tab label in the compact header', () => {
        context.setActiveTab('memories');
        fixture.detectChanges();

        const title = fixture.nativeElement.querySelector('.context-panel__header-title') as HTMLElement;
        expect(title?.textContent?.trim()).toBe('Memories');
        expect(fixture.nativeElement.textContent).not.toContain('Conversation context');
        expect(fixture.nativeElement.querySelector('[aria-label="Memory scope"]')).not.toBeNull();
    });
});

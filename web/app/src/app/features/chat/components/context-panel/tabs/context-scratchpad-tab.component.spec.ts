import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ChatService } from '../../../../../core/services/chat.service';
import { RightPanelService } from '../../../../../core/services/right-panel.service';
import { ContextPanelService } from '../../../services/context-panel.service';
import { ScratchpadService } from '../../../services/scratchpad.service';
import { ContextScratchpadTabComponent } from './context-scratchpad-tab.component';

describe('ContextScratchpadTabComponent', () => {
    let fixture: ComponentFixture<ContextScratchpadTabComponent>;

    beforeEach(async () => {
        const chatService = {
            getChatContext: vi.fn().mockName("ChatService.getChatContext"),
            patchChatContext: vi.fn().mockName("ChatService.patchChatContext")
        };
        chatService.getChatContext.mockReturnValue(of({
            chat_id: 'chat-1',
            active_scratchpad: 'note',
            summary: 'summary',
        }));
        chatService.patchChatContext.mockReturnValue(of({
            chat_id: 'chat-1',
            active_scratchpad: 'note',
            summary: 'summary',
        }));

        await TestBed.configureTestingModule({
            imports: [ContextScratchpadTabComponent],
            providers: [
                provideZonelessChangeDetection(),
                ScratchpadService,
                ContextPanelService,
                { provide: RightPanelService, useValue: {
                        setVisible: vi.fn().mockName("RightPanelService.setVisible")
                    } },
                { provide: ChatService, useValue: chatService },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(ContextScratchpadTabComponent);
        fixture.componentRef.setInput('chatId', 'chat-1');
        fixture.componentRef.setInput('canSave', true);
        fixture.detectChanges();
        await fixture.whenStable();
        fixture.detectChanges();
    });

    it('renders scratchpad editor for active thread', () => {
        const textarea = fixture.nativeElement.querySelector('textarea') as HTMLTextAreaElement | null;
        expect(textarea).not.toBeNull();
    });

    it('shows the active personality name in the scratchpad heading', () => {
        fixture.componentRef.setInput('personalityName', 'Aurex');
        fixture.detectChanges();

        const label = fixture.nativeElement.querySelector('label.label') as HTMLLabelElement | null;
        expect(label?.textContent?.trim()).toBe(`Aurex's Scratchpad`);
    });
});

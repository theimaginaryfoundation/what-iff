import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { RightPanelService } from './right-panel.service';

describe('RightPanelService', () => {
    let service: RightPanelService;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [provideZonelessChangeDetection(), RightPanelService],
        });
        service = TestBed.inject(RightPanelService);
    });

    it('defaults to hidden', () => {
        expect(service.visible()).toBe(false);
    });

    it('setVisible flips the signal', () => {
        service.setVisible(true);
        expect(service.visible()).toBe(true);
        service.setVisible(false);
        expect(service.visible()).toBe(false);
    });

    it('toggle flips the signal', () => {
        service.toggle();
        expect(service.visible()).toBe(true);
        service.toggle();
        expect(service.visible()).toBe(false);
    });
});

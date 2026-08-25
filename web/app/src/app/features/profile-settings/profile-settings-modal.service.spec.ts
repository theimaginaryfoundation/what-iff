import { ProfileSettingsModalService } from './profile-settings-modal.service';

describe('ProfileSettingsModalService', () => {
    let service: ProfileSettingsModalService;

    beforeEach(() => {
        service = new ProfileSettingsModalService();
    });

    it('opens to the requested tab', () => {
        service.open('billing');

        expect(service.openState()).toBe(true);
        expect(service.activeTab()).toBe('billing');
    });

    it('closes without resetting the selected tab', () => {
        service.open('usage');
        service.close();

        expect(service.openState()).toBe(false);
        expect(service.activeTab()).toBe('usage');
    });

    it('increments billingTabClosedRevision when closing from a billing-related tab', () => {
        service.open('profile');
        service.close();
        expect(service.billingTabClosedRevision()).toBe(0);

        service.open('billing');
        service.close();
        expect(service.billingTabClosedRevision()).toBe(1);

        service.open('subscription');
        service.close();
        expect(service.billingTabClosedRevision()).toBe(2);
    });

    it('updates the active tab while open', () => {
        service.open();
        service.setTab('subscription');

        expect(service.openState()).toBe(true);
        expect(service.activeTab()).toBe('subscription');
    });
});

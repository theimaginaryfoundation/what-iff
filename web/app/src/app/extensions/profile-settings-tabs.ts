/**
 * Extra Profile & Settings tabs (swap-point file).
 *
 * The open-source build adds none, so the union is `never` and
 * `ProfileSettingsTab` in `features/profile-settings/profile-settings-modal.service.ts`
 * stays exactly its base members. A private build replaces this file with the
 * ids of the tabs it adds (for example a marketplace tab) and, since the
 * modal's template is replaced wholesale by the overlay, renders them there.
 * Keeping the id union here means `ProfileSettingsModalService.open(tab)` and
 * `setTab(tab)` accept those ids without the service knowing about them.
 */
export type ExtensionProfileSettingsTab = never;

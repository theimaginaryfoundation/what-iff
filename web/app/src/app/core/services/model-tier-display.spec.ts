import { HiddenModelTierDisplay, ShownModelTierDisplay } from './model-tier-display';

// Both answers are pinned here so the build that hides tiers still proves the
// showing implementation works. Otherwise the only place it runs is the build
// this repo does not compile.
describe('ModelTierDisplay', () => {
  it('hides tiers by default', () => {
    expect(new HiddenModelTierDisplay().enabled()).toBe(false);
  });

  it('shows them when a build binds the showing implementation', () => {
    expect(new ShownModelTierDisplay().enabled()).toBe(true);
  });
});

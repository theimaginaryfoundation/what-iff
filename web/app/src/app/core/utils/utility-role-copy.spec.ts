import { utilityRoleUnavailableMessage } from './utility-role-copy';

describe('utilityRoleUnavailableMessage', () => {
  it('names the role and where to fix it, where the account chooses', () => {
    expect(utilityRoleUnavailableMessage('utility_model_not_set', 'personality', true)).toBe(
      'No model is chosen for Personality generation. Choose one in Settings, under Background models.',
    );
    expect(utilityRoleUnavailableMessage('utility_role_off', 'simple', true)).toContain('Simple tasks is switched off.');
    expect(utilityRoleUnavailableMessage('utility_model_unavailable', 'simple', true)).toContain("The model chosen for Simple tasks can't be used any more.");
  });

  it('only informs where the server chooses', () => {
    expect(utilityRoleUnavailableMessage('utility_model_not_set', 'simple', false)).toBe("This server hasn't chosen a model for Simple tasks.");
  });

  it('leaves any other failure to the caller', () => {
    expect(utilityRoleUnavailableMessage('provider_not_configured', 'simple', true)).toBeNull();
    expect(utilityRoleUnavailableMessage(undefined, 'simple', true)).toBeNull();
  });
});

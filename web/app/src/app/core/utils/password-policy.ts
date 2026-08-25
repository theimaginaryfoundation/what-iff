import type { AbstractControl, ValidationErrors } from '@angular/forms';

export type PasswordRequirementKey = 'minLength' | 'lowercase' | 'uppercase' | 'number' | 'symbol';

export interface PasswordPolicy {
  minLength: number;
  requireLowercase: boolean;
  requireUppercase: boolean;
  requireNumber: boolean;
  requireSymbol: boolean;
}

export type PasswordPolicyResult = Record<PasswordRequirementKey, boolean>;

const REQUIREMENT_ORDER: PasswordRequirementKey[] = ['minLength', 'lowercase', 'uppercase', 'number', 'symbol'];

export const PASSWORD_REQUIREMENT_LABELS: Record<PasswordRequirementKey, (policy: PasswordPolicy) => string> = {
  minLength: (policy) => `At least ${policy.minLength} characters`,
  lowercase: () => 'One lowercase letter',
  uppercase: () => 'One uppercase letter',
  number: () => 'One number',
  symbol: () => 'One special character'
};

const REQUIREMENT_ENABLED: Record<PasswordRequirementKey, (policy: PasswordPolicy) => boolean> = {
  minLength: () => true,
  lowercase: (policy) => policy.requireLowercase,
  uppercase: (policy) => policy.requireUppercase,
  number: (policy) => policy.requireNumber,
  symbol: (policy) => policy.requireSymbol
};

export function enabledPasswordRequirements(policy: PasswordPolicy): PasswordRequirementKey[] {
  return REQUIREMENT_ORDER.filter((key) => REQUIREMENT_ENABLED[key](policy));
}

export function evaluatePasswordPolicy(value: string, policy: PasswordPolicy): PasswordPolicyResult {
  return {
    minLength: value.length >= policy.minLength,
    lowercase: !policy.requireLowercase || /[a-z]/.test(value),
    uppercase: !policy.requireUppercase || /[A-Z]/.test(value),
    number: !policy.requireNumber || /\d/.test(value),
    symbol: !policy.requireSymbol || /[^A-Za-z0-9]/.test(value)
  };
}

export function getPasswordPolicyDescription(policy: PasswordPolicy): string {
  const parts: string[] = [];

  parts.push(`at least ${policy.minLength} characters`);
  if (policy.requireLowercase) parts.push('one lowercase letter');
  if (policy.requireUppercase) parts.push('one uppercase letter');
  if (policy.requireNumber) parts.push('one number');
  if (policy.requireSymbol) parts.push('one special character');

  if (parts.length === 1) {
    return `Password must include ${parts[0]}.`;
  }

  const last = parts.pop();
  return `Password must include ${parts.join(', ')}, and ${last}.`;
}

export function createPasswordPolicyValidator(policy: PasswordPolicy) {
  return (control: AbstractControl): ValidationErrors | null => {
    const value = control.value ?? '';

    if (!value) {
      return null;
    }

    const evaluation = evaluatePasswordPolicy(value, policy);
    const failures = Object.entries(evaluation).reduce<Partial<Record<PasswordRequirementKey, true>>>((acc, [key, passed]) => {
      if (!passed) {
        acc[key as PasswordRequirementKey] = true;
      }
      return acc;
    }, {});

    return Object.keys(failures).length > 0 ? { passwordPolicy: failures } : null;
  };
}

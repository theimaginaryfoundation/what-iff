import { AgentJobParameter } from '../../../core/models/agent-job.model';
import { ConfirmationOptions } from '../../../core/services/confirmation.service';
import { joinNames, parameterLabels } from './job-params.helpers';

/** "Topics is empty" / "Topics and Region are empty". */
function emptyPhrase(defs: AgentJobParameter[], missing: string[]): string {
  const labels = parameterLabels(defs, missing);
  return `${joinNames(labels)} ${labels.length === 1 ? 'is' : 'are'} empty`;
}

/**
 * The warning before saving a job with parameters that have no value (ADR 0x044, decision 7), or
 * null when every parameter has one. Confirming saves anyway.
 */
export function saveWithMissingValuesDialog(defs: AgentJobParameter[], missing: string[]): ConfirmationOptions | null {
  if (missing.length === 0) return null;
  return {
    title: 'Some parameters have no value',
    message: `${emptyPhrase(defs, missing)}. You can save now, but this job can't run until every parameter has a value.`,
    type: 'warning',
    confirmText: 'Save anyway',
    cancelText: 'Back to editing',
  };
}

/**
 * The question before leaving the job page (ADR 0x044, decision 8), or null when there is nothing
 * to ask: unsaved edits, a saved job that still needs values, or both in one dialog. Confirming
 * leaves.
 */
export function leaveJobDialog(unsavedEdits: boolean, defs: AgentJobParameter[], missing: string[]): ConfirmationOptions | null {
  if (unsavedEdits && missing.length > 0) {
    return {
      title: 'Discard unsaved changes?',
      message: "Your changes to this job haven't been saved, and it still can't run until every parameter has a value.",
      type: 'warning',
      confirmText: 'Discard changes',
      cancelText: 'Keep editing',
    };
  }
  if (unsavedEdits) {
    return {
      title: 'Discard unsaved changes?',
      message: "Your changes to this job haven't been saved.",
      type: 'warning',
      confirmText: 'Discard changes',
      cancelText: 'Keep editing',
    };
  }
  if (missing.length > 0) {
    return {
      title: 'This job needs setup',
      message: `${emptyPhrase(defs, missing)}. This job can't run until every parameter has a value.`,
      type: 'warning',
      confirmText: 'Leave anyway',
      cancelText: 'Stay and fill them in',
    };
  }
  return null;
}

import { AgentJobParameter } from '../../../core/models/agent-job.model';
import { leaveJobDialog, saveWithMissingValuesDialog } from './job-dialogs.helpers';

const defs: AgentJobParameter[] = [
  { name: 'topics', label: 'Topics', type: 'list' },
  { name: 'region', type: 'single_select', options: ['US'] },
];

describe('job dialogs', () => {
  it('asks before saving with missing values, naming them by label', () => {
    expect(saveWithMissingValuesDialog(defs, [])).toBeNull();
    const dialog = saveWithMissingValuesDialog(defs, ['topics', 'region'])!;
    expect(dialog.message).toContain('Topics and region are empty');
    expect(dialog.confirmText).toBe('Save anyway');
    expect(dialog.cancelText).toBe('Back to editing');
    expect(saveWithMissingValuesDialog(defs, ['topics'])!.message).toContain('Topics is empty');
  });

  it('asks before leaving for unsaved edits, missing values, or both', () => {
    expect(leaveJobDialog(false, defs, [])).toBeNull();
    expect(leaveJobDialog(true, defs, [])!.title).toBe('Discard unsaved changes?');
    const setup = leaveJobDialog(false, defs, ['topics'])!;
    expect(setup.title).toBe('This job needs setup');
    expect(setup.cancelText).toBe('Stay and fill them in');
    expect(leaveJobDialog(true, defs, ['topics'])!.message).toContain("can't run until every parameter has a value");
  });
});

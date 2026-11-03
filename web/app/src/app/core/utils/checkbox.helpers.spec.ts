import { saveCheckbox } from './checkbox.helpers';

describe('saveCheckbox', () => {
  function box(checked: boolean): HTMLInputElement {
    const input = document.createElement('input');
    input.type = 'checkbox';
    input.checked = checked;
    return input;
  }

  it('passes the new state to the save and leaves the box when it succeeds', async () => {
    const input = box(true);
    const save = vi.fn(async () => true);
    expect(await saveCheckbox(input, save)).toBe(true);
    expect(save).toHaveBeenCalledWith(true);
    expect(input.checked).toBe(true);
  });

  it('puts the box back when the save fails', async () => {
    const unticked = box(false);
    expect(await saveCheckbox(unticked, async () => false)).toBe(false);
    expect(unticked.checked).toBe(true);

    const ticked = box(true);
    await saveCheckbox(ticked, async () => false);
    expect(ticked.checked).toBe(false);
  });
});

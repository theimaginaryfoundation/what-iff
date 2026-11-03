/**
 * Runs the save behind a checkbox the user just clicked, and puts the box back when it fails.
 *
 * A template's `[checked]` binding only writes the DOM property when the bound value changes.
 * After a failed save the bound value is still what it was before the click, so Angular never
 * rewrites the box and it keeps showing the click rather than the saved state. `save` gets the
 * box's new state and reports whether it was saved (it shows its own error). The box is put back
 * before this returns, so a caller that reloads afterwards still ends on the server's value.
 */
export async function saveCheckbox(box: HTMLInputElement, save: (checked: boolean) => Promise<boolean>): Promise<boolean> {
  const checked = box.checked;
  const ok = await save(checked);
  if (!ok) box.checked = !checked;
  return ok;
}

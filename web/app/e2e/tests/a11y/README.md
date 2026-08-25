# A11y

`@axe-core/playwright` scans of each key page in its meaningful states (empty,
populated, modal open). The gate fails on `serious` and `critical` violations
and reports `moderate`/`minor` without failing; known issues live in one
shared allowlist with a linked issue per entry.

## What is here now

`gallery.a11y.spec.ts` — the segmented controls, mode switch and accessible
names on `/gallery`. These are state-exposure assertions rather than scans:
they check that the chosen filter, sort and mode are readable by something
other than a sighted user, which is exactly what the page failed to do when
its selected state lived only in a CSS class.

Specs of that kind belong here rather than in `tests/functional/`, which
covers what the feature does. The split is worth keeping: a functional test
of the gallery filters passed the whole time the selected state was
invisible to assistive tech.

The axe harness above is still unbuilt — `@axe-core/playwright` is not a
dependency yet, and there is no allowlist. Adding it does not require moving
anything already in this directory.

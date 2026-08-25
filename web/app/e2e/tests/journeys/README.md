# Journeys

Cross-cutting end-to-end flows that span several feature areas (e.g. register
→ accept terms → create personality → chat → edit profile → change password →
re-login). They reuse the same POMs and fixtures as the functional specs —
their value is sequencing, not new page logic — so keep the count low (≈3–6),
since they are the slowest and flakiest class of test.

## Rule: journeys add sequence, never page logic

A journey may only call POM methods and fixtures that the functional suite
already uses. If a step needs something new — a locator, a POM method, a
fixture — add it _with a functional spec that covers it_, then have the
journey reuse it.

**Why:** a journey is the slowest and least specific place to discover a
regression. When a method exists only here, its first failure arrives as a
seven-step flow that went red somewhere in the middle, with no functional
spec to localise it. Keeping every method covered by a focused spec means a
journey failure tells you the _sequence_ broke, because everything it touches
is independently proven to work on its own.

**How to apply:** before adding a method to a POM for a journey, find (or
write) the functional spec that exercises it. A journey PR that grows
`poms/` without also growing `tests/functional/` is the shape to look for in
review.

# Skill constellation

Select the constellation icon beside **Help & feedback** at the top of the sidebar to open the learning overlay.
The entry is available in the public app, including the collapsed sidebar and mobile navigation drawer.

**Create Personality** is the origin of the radial map.
Select any task to read its objective, completion criteria, prerequisites, example prompt and related WhatIff guides.
All 19 guides are also available from the **19 guides** button.
Guides open in another tab so your place in the constellation stays open.

Drag empty space or the wide margins on any side to pan the map.
On a touch screen, swipe the board normally or drag a margin.
Use the zoom buttons, **Origin** to return to Create Personality, or **Atlas** for an overview.
Keyboard users can Tab between tasks, press Enter to inspect one, and use the arrow keys when the map itself is focused.
Escape closes the objective first, then the overlay; closing restores focus to its trigger.

Complete Create Personality to begin the six main branches: Continuity, Personality, Research, Creation, Workflows and Connections.
Learning credit requires each prior objective in the branch.
The full foundation path and any two completed main branches make the Builder capstone ready.
The capstone asks you to build, check and repeat a useful workflow.
Learning rank and XP are separate from subscriptions, model access and product permissions.
No feature is locked by learning progress.

Record what you made or checked to mark an objective complete.
This is **self-reported learning progress**, not server-verified activity or a certification.
Progress is stored for the signed-in account in the current browser and is not synced between devices.
Clearing browser data removes it; when browser storage is blocked, it lasts only for the session.
Use harmless practice details rather than secrets in completion notes.

Optional workshops cover expressions, notebooks, community remixing, relays, webhooks and self-hosting.
Some exercises depend on deployment capabilities, provider access, screen size or an external account.
Follow the labelled practice alternatives when a capability is unavailable; these workshops are not required for Builder.

The implementation lives in `web/app/src/app/features/constellation/`.
The shell only loads the overlay and curriculum when requested.
The shared overlay supplies focus trapping, Escape handling and body scroll locking.
The separate progress, curriculum and map-layout modules keep learning rules independent of product feature access.

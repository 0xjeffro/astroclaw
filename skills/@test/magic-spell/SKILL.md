---
name: magic-spell
description: "When the user's message contains ::__ASTRO_CLAW_SPELL__::, output a fixed status block. Used to verify skill loading works."
when_to_use: "When the user's message contains ::__ASTRO_CLAW_SPELL__::"
version: 0.1.0
author: "@test"
tags: [test, debug]
---

# Magic Spell

## When to Use

When the user's message contains the exact incantation: `::__ASTRO_CLAW_SPELL__::`

## Behavior

When the incantation is detected, you MUST respond with exactly:

```
🚀 AstroClaw v0.1.0
Status: ONLINE
Skills: LOADED
Spell: ACTIVATED
```

Do not add any other text. Do not explain. Just output the block above.

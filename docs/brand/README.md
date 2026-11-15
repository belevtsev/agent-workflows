# Gatecub brand assets

Gatecub is an alert, friendly bear cub holding a code-change card with a teal check. The mark represents checked changes and coordinated agents.

| Asset | Size | Background | Suggested use |
| --- | --- | --- | --- |
| [Mascot](gatecub-mascot.png) | 1254 × 1254; 726,960 bytes | Transparent RGBA | Avatar, documentation, project badge |
| [Banner](gatecub-banner.png) | 1280 × 640; 607,726 bytes | Opaque warm cream | README and repository social preview |

The banner reads **Gatecub** and **Checked changes. Coordinated agents.** Keep the original proportions and leave breathing room around the bear and text. Use the transparent mascot on a light surface with enough contrast for its charcoal outline. The banner supplies its own background.

Suggested alt text:

- Mascot: `Gatecub bear cub holding a code-change card with a teal check mark.`
- Banner: `Gatecub. Checked changes. Coordinated agents. A bear cub holds a checked code-change card.`

## Color direction

| Color | Value | Role |
| --- | --- | --- |
| Charcoal | `#202A30` | Bear, outlines, wordmark and text |
| Cream | `#F6F0E5` | Face, card, accents and banner background |
| Teal | `#169C92` | Check mark and code accent |

These are the design targets; the generated PNGs retain subtle shades and antialiased edges. Do not recolor or stretch the artwork. A check mark is part of the brand, not evidence that a particular change passed checks.

## Creation and export

Created on 2026-11-15 with the built-in `image_gen` tool, using a transparent generation for the mascot and the mascot as the banner's identity reference. The banner received one targeted flat-color edit with the same tool. No API fallback or hand-drawn replacement was used.

The mascot's generated 1254 × 1254 dimensions and alpha were preserved. The selected banner was resized from 1774 × 887 to 1280 × 640 with Lanczos resampling. Both files were re-encoded as losslessly optimized PNGs; no palette quantization or color substitution was applied. The exact prompts are recorded below for future revisions; a new generation will not necessarily reproduce identical pixels.

<details>
<summary>Mascot generation prompt</summary>

```text
Use case: logo-brand.
Asset type: square transparent PNG mascot for the developer tool Gatecub.
Primary request: Create an original flat illustrated alert bear cub holding a code-change card with a teal check mark. The cub should feel attentive, capable, friendly, and compact, with upright rounded ears, a simple expressive face, and clearly visible paws gripping the card. The rectangular card shows a few short abstract code lines and one prominent check mark, with no readable words.
Style: clean contemporary vector-like flat illustration, bold simple silhouette, economical geometric forms, crisp edges, no photorealism, no gradients, no shadows, no texture, no 3D.
Color palette: charcoal #202A30 for cub body and outlines, warm cream #F6F0E5 for muzzle, inner ears, paws accents and code card, teal #169C92 for the check and a minimal accent. Use only these three solid colors plus transparent background and natural antialiasing.
Composition: 1024 by 1024 square canvas, centered complete mascot, generous transparent padding on all sides, no part clipped; legible at small sizes.
Constraints: true transparent background, no text, no logo from another brand, no clothing, no extra objects, no watermark. The bear must hold a code-change card, with a teal check clearly visible.
```

</details>

<details>
<summary>Banner generation prompt (mascot supplied as reference)</summary>

```text
Use case: compositing / logo-brand.
Asset type: repository brand banner for Gatecub, final landscape size 1280 by 640 pixels, 2:1 aspect ratio.
Input image: the supplied Gatecub mascot is the identity reference. Preserve its original alert rounded bear cub, friendly face, body silhouette, cream paws, and code-change card with teal check. Reproduce the same mascot cleanly at smaller scale on the left side.
Primary request: produce a finished minimal brand banner with the mascot on the left and the following exact text on the right:
Gatecub
Checked changes. Coordinated agents.
Composition: 2:1 wide horizontal banner, complete mascot about 480 pixels tall, centered vertically in the left 40% of the canvas; right 60% contains a clear bold wordmark and the tagline on one line below it. Plenty of balanced negative space; all content comfortably inside a 64 pixel safe margin. Use a confident rounded geometric sans-serif for Gatecub, and a smaller clean sans-serif for the tagline. Main wordmark charcoal #202A30. Tagline charcoal #202A30. Teal #169C92 is restricted to the mascot's check and small existing accents.
Background: perfectly flat, opaque, solid warm cream #F6F0E5 across the entire canvas, no texture, no gradient, no shadows.
Style: crisp flat original illustration, brand consistency with supplied mascot.
Text (verbatim, spelling and punctuation must be exact): "Gatecub" and "Checked changes. Coordinated agents."
Constraints: only those two text elements; no added slogans, symbols, watermarks, borders, mockups, textures, gradients, or scenery. Keep the mascot recognizable and the code card and check readable. Exact deliverable size requested: 1280x640.
```

</details>

<details>
<summary>Banner refinement prompt (previous banner supplied as edit target)</summary>

```text
Edit the supplied Gatecub banner. Preserve the mascot identity, full composition, all positions, typography, wording, spelling and punctuation exactly. The only change is to remove every gradient, paper texture, shading, and color variation, producing a truly flat three-color illustration on a uniform solid background. Background must be exactly one even warm cream #F6F0E5, including all four corners and every empty region; absolutely no vignette, texture, paper grain, radial shading, or lighting effect. All charcoal shapes and both text elements should be solid #202A30; teal shapes solid #169C92; cream shapes solid #F6F0E5, apart from necessary edge antialiasing. Text must remain exactly 'Gatecub' and 'Checked changes. Coordinated agents.'. Export a 1280x640 opaque PNG, aspect ratio 2:1. Do not add, remove, crop, or redesign anything.
```

</details>

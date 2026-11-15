# Gatecub brand assets

Gatecub's mascot is a fluffy Pomeranian in a red knit beanie, holding a code-change card with a teal check. Its pointed ears, short dog muzzle, full neck ruff and curled tail make the dog recognizable; its calm, proud expression follows the user's reference.

| Asset | Size | Background | Suggested use |
| --- | --- | --- | --- |
| [Mascot](gatecub-mascot.png) | 1254 × 1254; 923,432 bytes | Transparent RGBA | Avatar, documentation, project badge |
| [Banner](gatecub-banner.png) | 1280 × 640; 586,883 bytes | Opaque warm cream | README and repository social preview |

The banner reads **Gatecub** and **Checked changes. Coordinated agents.** Keep the original proportions and leave breathing room around the dog and text. Use the transparent mascot on a light surface with enough contrast for its charcoal outline. The banner supplies its own background.

Suggested alt text:

- Mascot: `Gatecub Pomeranian in a red knit beanie, holding a code-change card with a teal check mark.`
- Banner: `Gatecub. Checked changes. Coordinated agents. A fluffy Pomeranian in a red beanie holds a checked code-change card.`

## Color direction

| Color | Value | Role |
| --- | --- | --- |
| Charcoal | `#202A30` | Outlines, dark fur, eyes, nose, wordmark and text |
| Cream | `#F6F0E5` | Light fur, paws, card and banner background |
| Teal | `#169C92` | Check mark and code accent |
| Red | `#C83D37` | Knit beanie, inspired by the user's reference |

These are the design targets; the generated PNGs retain subtle shades and antialiased edges. Keep the beanie as the red accent and the card check teal. Do not recolor or stretch the artwork.

## Creation and export

Updated on 2026-11-15 with the built-in `image_gen` tool. The mascot was edited from the previous Gatecub mascot using the user's Pomeranian image as species and mood guidance. The matching banner was edited from the previous banner with the new Pomeranian mascot as its identity reference. All creative changes were made with `image_gen`; no API fallback or procedural drawing was used. The private input reference is not included in this repository.

The mascot's generated 1254 × 1254 dimensions and alpha were preserved. The selected banner was resized from 1774 × 887 to 1280 × 640 using the browser's high-quality image resampling. Both PNGs were then losslessly re-encoded with Go's PNG encoder at its best compression setting. No palette quantization or color substitution was applied. Both final files are under 1 MB.

The exact edit prompts that produced the current artwork follow. A new generation will not necessarily reproduce identical pixels.

<details>
<summary>Pomeranian mascot edit prompt</summary>

```text
Use case: precise-object-edit / logo-brand.
Asset type: Gatecub developer-tool mascot, square transparent PNG.
Input image 1 is the current mascot edit target. Input image 2 is the user's Pomeranian reference: use its fluffy dog face and ruff, small dark eyes, canine muzzle, calm proud expression, and red knit beanie as species and mood guidance. Do not reproduce the reference's photographic texture or dark background.
Primary request: Replace the bear completely with an original, clearly recognizable Pomeranian dog. Give the dog a fluffy cream-and-charcoal spitz face, broad layered neck ruff, short pointed canine muzzle with a small black nose, and small pointed triangular ears partly tucked beneath a red knit beanie. The beanie has a folded ribbed cuff and a few simple knit lines. The expression is calm, capable, friendly and a little proud, inspired by the reference. There must be no round bear ears or bear face.
Preserve the useful compact seated mascot composition: a complete small fluffy dog with visible front paws gripping a cream code-change card, little hind paws, and a curled fluffy tail where it fits naturally. The card has a few abstract short code lines, a small code-bracket symbol, and one prominent teal check mark; no readable words.
Style: clean flat original brand illustration, crisp vector-like contours, simplified tufts of fur, economical geometric forms, legible silhouette at small sizes. No photorealism, no detailed fur rendering, no 3D, no scenery, no cast shadows, no watermark.
Color direction: charcoal #202A30 for outlines, dark fur accents, eyes and nose; warm cream #F6F0E5 for the main fur, paws and code card; teal #169C92 for the check and code accent; tasteful red #C83D37 for the beanie. Natural antialiasing is fine.
Composition: square canvas, full mascot centered, generous genuinely transparent padding on all four sides. The full beanie, fur, paws, card and tail stay inside the frame. Transparent background, no text. Keep the card and check visually clear.
```

</details>

<details>
<summary>Matching banner edit prompt</summary>

```text
Use case: precise-object-edit / logo-brand.
Asset type: Gatecub repository banner, opaque warm-cream landscape PNG, 2:1 aspect ratio, final target 1280x640.
Input image 1 is the current Gatecub banner edit target. Input image 2 is the approved new Pomeranian mascot identity reference.
Primary request: replace only the bear mascot on the left with the exact Pomeranian identity from input image 2: fluffy cream-and-charcoal face and neck ruff, small pointed dog ears, short canine muzzle, calm proud friendly expression, red knit beanie with folded ribbed cuff, front paws holding the cream code-change card with teal code brackets and check, little hind paws, and curled fluffy tail. Keep the complete dog inside the frame. The two assets must unmistakably depict the same mascot.
Preserve the current banner's typography, wordmark, tagline, spelling, punctuation, spacing, balanced left-mascot/right-text composition, and warm cream background. Scale the new mascot to fit comfortably within the existing left area with breathing room; its ears, beanie and tail must not touch the wordmark or the frame.
Text (verbatim, exactly these two elements): "Gatecub" and "Checked changes. Coordinated agents."
Style: crisp clean flat brand illustration matching input image 2, simplified fur tufts, no photorealism, no 3D, no extra symbols or scenery. Keep the teal check visually prominent.
Color direction: charcoal #202A30 for text, outlines and dark fur; warm cream #F6F0E5 for the background, light fur and card; teal #169C92 for card accents; red #C83D37 for the beanie. The background should read as a plain even warm cream, with no added texture, gradient or shadow.
Constraints: no remaining bear features, no round ears, no new words, no altered lettering, no watermark. Final composition remains wide 2:1 with all content safely inside the edges. Target output 1280 by 640 pixels.
```

</details>

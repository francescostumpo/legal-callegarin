# Public asset provenance

## Editorial image library

The public site ships exactly 12 first-party editorial image IDs. The source
compositions were generated on 11 September 2026 with the built-in Codex image
generation tool. The approved mockup was supplied only as a style and
composition reference; it was not edited or included in the outputs.

All prompts shared this direction: photorealistic editorial or architectural
photography; graphite and dark stone, walnut, ivory paper, highly restrained
copper; warm natural grazing light; real material texture; calm premium
composition and useful negative space. Every prompt expressly excluded people,
faces, bodies, hands, silhouettes, readable text, logos, watermarks, gavels,
scales, handshakes, robes, flags, signage, and generic courthouse imagery.

| ID                   | Subject                                    | Usage                     |
| -------------------- | ------------------------------------------ | ------------------------- |
| `hero-architecture`  | Walnut slats and charcoal stone planes     | Home hero                 |
| `approach-library`   | Untitled clothbound books in grazing light | Approach                  |
| `family-objects`     | Two neutral folders and two empty seats    | Family and persons        |
| `succession-seal`    | Blank envelope and plain copper seal       | Inheritance and donations |
| `contracts-pen`      | Fountain pen on blank paper                | Obligations and contracts |
| `debt-ledger`        | Unwritten ledger and unmarked brass ruler  | Debt recovery             |
| `damages-road`       | Empty wet road at dusk                     | Compensation for damages  |
| `property-key`       | Key on interlocking stone geometry         | Property rights           |
| `criminal-threshold` | Empty protective architectural threshold   | Criminal law              |
| `tax-ledger`         | Blank geometric sheets and counting rods   | Tax law                   |
| `article-notebook`   | Blank open notebook and pen                | Judgments and reflections |
| `contact-entrance`   | Anonymous architectural entrance           | Contacts                  |

The generated PNG bases were copied into the task worktree for inspection but
are not runtime dependencies. Sharp `0.35.4`, pinned in `package-lock.json`,
created a `1600×900` landscape crop and a `900×1125` card crop in AVIF and WebP.
AVIF uses quality 50/effort 6; WebP uses quality 70/effort 6 and smart
subsampling. Sharp strips EXIF, IPTC, and XMP by default. The build-time script
`scripts/verify-assets.mjs` reads real file metadata and rejects missing,
duplicate, unlisted, oversized, wrongly dimensioned, or metadata-bearing files.

Both source compositions and landscape/card contact sheets were visually
reviewed. No human subject, prohibited legal cliché, third-party mark, or
readable text was found. All 48 served derivatives are below 200 KiB; the
largest is 110,050 bytes, below the 300 KiB hard limit.

## Self-hosted fonts

The browser makes no remote font request. Both variable fonts and their license
texts live in `internal/webassets/public/fonts/` and are embedded in the Go
binary.

| Family        | Local file                           | Package provenance                                             | Source font version                         | License                              |
| ------------- | ------------------------------------ | -------------------------------------------------------------- | ------------------------------------------- | ------------------------------------ |
| Fraunces      | `fraunces-latin-variable.woff2`      | `@fontsource-variable/fraunces@5.3.0` from npm/Fontsource      | v38, Google Fonts source updated 2025-09-10 | SIL OFL 1.1, `OFL-Fraunces.txt`      |
| Source Sans 3 | `source-sans-3-latin-variable.woff2` | `@fontsource-variable/source-sans-3@5.3.0` from npm/Fontsource | v19, Google Fonts source updated 2025-09-05 | SIL OFL 1.1, `OFL-Source-Sans-3.txt` |

Upstream provenance recorded by the pinned packages:

- [Fontsource Fraunces package](https://www.npmjs.com/package/@fontsource-variable/fraunces)
- [Fontsource Source Sans 3 package](https://www.npmjs.com/package/@fontsource-variable/source-sans-3)
- [Google Fonts repository](https://github.com/google/fonts)

The CSS fallback stacks use locally available serif and system sans-serif
families when a WOFF2 file cannot be loaded.

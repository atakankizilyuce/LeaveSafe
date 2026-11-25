# Offline font coverage

The seven Noto font families in `web/public/fonts` are bundled and served by the
daemon. No font CDN or other network request is required. The initial regular
instances came from the reviewed LeaveSafe application font pipeline at app
commit `df0a22a`; pinned upstream revisions, hashes, and license locations are in
`font-sources.json`. The corresponding license files ship beside the fonts.
Browsers synthesize weight from these regular faces when the system font does
not cover a script.

The fonts can be rebuilt with `fonts.py --source-dir SOURCE`; the CJK subset
must be regenerated when the glyph coverage check finds new translated characters.
Run `fonts.py --check` against the final catalogs before release. Source font bytes are available from the pinned
upstream hashes and must be verified before regeneration. The language review
and glyph coverage check are separate requirements.

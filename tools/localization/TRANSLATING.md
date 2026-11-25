# Browser panel translation contract

The canonical source is `web/src/locales/en.json`. Every source key must exist in
`web/src/locales/<BCP47>.json`, with exactly the same key spelling. Keys are source
messages; translate values only. The 25 locale tags are listed in
`web/src/lib/i18n.ts`. The canonical script tags use hyphens (`zh-Hans`,
`pnb-Arab`, `yue-Hant`).

Translate complete messages naturally, preserving the practical action, security
meaning, and limitations. Protection/arming means monitoring sensors, not locking
the operating system. Disarming stops monitoring. Silencing/dismissing an alarm
does not itself disarm. Pairing keys here are local daemon connection keys, not
cloud account keys. This browser panel has no account/cloud service. Do not copy
mobile application's cloud delivery promises into this panel. The browser may
be suspended when the phone locks; the laptop continues local monitoring.

Named `{variables}` must be preserved exactly and can move to suit grammar.
Never translate commands, configuration identifiers, package names, product names,
or URLs. Technical identifiers embedded in explanations remain literal.

A message can be a string or a plural object. For plural objects, `other` is
required; add any applicable `zero`, `one`, `two`, `few`, `many`, or exact `=0`,
`=1` keys. Each value is a complete message. `{count}` is the number controlling
plural choice. Numeric values are formatted in the selected locale. All plural
branches preserve all source variables. These are JSON plural objects, **not**
ICU expressions. Keep time abbreviations conventional and unambiguous in the
language. Do not substitute English for missing translations.

Some initial translations are reused from `leavesafe-app/lib/l10n/app_*.arb`.
`tools/localization/app-mapping.json` records the matching app keys. These are
starting points, not proof that a whole daemon catalog has been translated or
independently reviewed. Verify reused strings in the daemon context too.

Only complete catalogs are offered in the picker. Structural validation is
separate from the requested independent language review. Translation authors
must not mark their own catalog independently reviewed. Do not edit another
language, shared source, code, generated dist, or review files for another agent.

## Terminal catalogs

The separate Go terminal source is `internal/localization/catalogs/en.json`
(87 messages; source SHA256
`53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`).
Translate values into the same locale filename. Terminal messages use Go `fmt`
verbs such as `%s`, `%d`, `%q`, and `%v`, **not** browser `{variables}`. Preserve
all formatting verbs and their argument order, including color/reset `%s`
arguments. Preserve commands (`arm`, `disarm`, `qr`, `help`, `rotate-key`), CLI
flags, config field names, `[ARM]`/`[KEY]` diagnostic markers, and URLs. Newlines
and indentation are layout, not prose. A `%s` around a word is often ANSI color
rather than user data; consult the call in `cmd/leavesafe` if ambiguous. Terminal
formulations should be concise enough for narrow windows. The long help message
is one complete unit, including command descriptions; do not translate its
literal command names or filenames.

Platform autostart instructions are frozen separately in
`internal/localization/service/en.json` (28 messages; source SHA256
`41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`).
Each terminal language requires **both** its 87-message main catalog and this
28-message service catalog. Translate human headings and instructions, retaining
OS tools, file names and commands. The macOS paragraph must use the locale's
actual System Settings → Privacy & Security → Accessibility names. Windows
Scheduled Task describes a user logon task, not a Windows service. Linux linger
instructions describe running after logout and starting at boot; do not imply
this happens without the user running the shown command.

## Independent review record

A different independent agent is required for each language. After reading all
194 browser, 87 terminal and 28 service messages, that reviewer writes
`docs/localization-reviews/<BCP47>.md` with scope, corrections and limits and a
JSON companion with exactly `locale`, `reviewer`, `sources` and `files` fields.
`sources` maps each of the three English source paths above to its file-byte
SHA256; `files` maps the three corresponding target paths to their SHA256.
Use repository-relative paths as keys. For English, source and target paths are
the same. Hash after corrections, and never sign your own authored translation.
`tools/localization/reviews.py` rejects stale or reused review evidence;
`--require-all` requires all 25 current independent reviews.

# Localization

The terminal and embedded browser panel each have a language preference. The
terminal stores its choice in `config.json`; `lang` changes it immediately. The
standalone help and service commands use `LEAVESAFE_LANGUAGE`, then `LC_ALL`,
`LC_MESSAGES`, and `LANG`. The browser follows the device's preferred languages
unless its language picker stores an explicit choice locally. Neither choice
changes arming, authentication, encryption, or account settings.

The target set is 25 languages. Catalogs are in `web/src/locales` (194 messages),
`internal/localization/catalogs` (87 terminal messages), and
`internal/localization/service` (28 platform autostart messages). Incomplete
catalogs are withheld from the relevant picker. A partially translated language
must not be described as supported. The language authoring and placeholder
contracts are in `tools/localization/TRANSLATING.md`.

The browser supports right-to-left layout, locale-specific numeric/time
formatting, full-sentence plurals, translated sensor alerts, and a persistent
service-worker notification language. Device names and unknown diagnostic text
remain verbatim. Local font files cover the target scripts; see
`tools/localization/FONTS.md`. Terminal column measurement accounts for CJK and
combining characters; shaping and bidirectional rendering depend on the terminal.
Command names, configuration keys, paths, OS output and protocol identifiers
remain stable.

Validation:

```sh
python3 tools/localization/check.py
python3 -m unittest discover -s tools/localization -p '*_test.py'
python3 tools/localization/fonts.py --check
python3 tools/localization/assets.py --check
npm --prefix web run typecheck
npm --prefix web run coverage
npm --prefix web run build
go test ./internal/localization ./internal/config ./cmd/leavesafe
```

`check.py --allow-incomplete` is for translation work only; CI requires every
catalog. Rebuild `web/dist` after catalog or source changes because the daemon
embeds committed build output. Regenerate fonts using verified pinned inputs
with `fonts.py --source-dir SOURCE` if new characters are introduced. All
translations require the independently assigned language review before release.

The pre-existing browser transport still sends the old raw-key auth request,
while the daemon requires v2 proof and encrypted transport. Localization does
not repair that protocol mismatch or weaken authentication. An end-to-end
browser pairing claim requires the separately scoped v2 transport work. The PWA install manifest
and page description use the selected language’s translated product tagline.

# French daemon independent review

PASS — 2026-10-18. Reviewer: `/root/review_web_fr`, independent of the catalog author.

Read and compared every entry: 194 browser messages, 87 terminal messages including the complete multiline help, and 28 platform-service messages (309 total). Reviewed all plural branches, sensor events, arming/disarming versus dismissing an alert, local pairing keys versus account keys, phone sleep/connectivity limits, update behavior, units and configurable thresholds.

Updated the browser input-sensor explanation to the final configured-sensitivity threshold source while retaining its original JSON key. Improved the connection explanation and the description of surveillance precision. Restored the omitted “in the configuration” instruction in two terminal settings messages. Existing service translations accurately retain Windows logon scheduled tasks, the French macOS path Réglages Système → Confidentialité et sécurité → Accessibilité, and the explicit Linux linger command requirement. Commands, flags, paths, URLs and diagnostic markers remain literal; no service catalog edits were needed.

Validation: all three exact source/target key sets (194/87/28), every named-variable multiset in every plural branch, nonempty values and ordered Go formatting verbs passed. The JSON companion records exact final source/target file-byte hashes.

Limits: independent agent linguistic review, not a native-human certification or a live OS walkthrough. Existing browser protocol compatibility, integrated rendering, builds, CI and Sonar remain integration concerns; this review does not claim they passed. No unresolved French meaning issue was found in this scope.


## Gallery and motion delta review — 2026-11-02

Independent reviewer: `/root/review_alarm_photo_languages/review_existing_fr`. This focused AI follow-up preserves all historical reports; prior reviewer identities remain historical and are not impersonated. It is not a fresh full-catalog review, native-human certification, physical-device test, legal-policy approval, or store/publication verification.

Read the three new browser entries independently against English and browser context, plus the translation contract and manifest schema. `MOUVEMENT` identifies physical movement; `L’appareil a bougé !` uses natural French punctuation (a space was added before the exclamation mark). The description monitors physical movement without location access, triggering on detected movement while protection is active; it does not describe a weapon, GPS tracking, or guaranteed detection of every movement. No platform support claim was added.

Baseline snapshot daemon commit `0d2dfbfe8b608557c947138bef2693b28494fc78`: all 194 existing browser values unchanged + three additions = 197. The 87 CLI and 28 service catalogs are unchanged both semantically against the snapshot and byte-for-byte against the baseline commit/previous manifest. Their hashes are retained as preservation evidence, not a fresh full linguistic review. Scoped browser placeholder/plural validation and `reviews.validate(locales={'fr'}, require_all=True)` passed. No CLI/service edit, generation, code, other locale, commit, or publication changed.

Reviewed file-byte SHA-256 hashes:

- `web/src/locales/en.json`: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- `internal/localization/catalogs/en.json`: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- `internal/localization/service/en.json`: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`
- `web/src/locales/fr.json`: `511a0016bb2b5250d08c18da1ebd4a365193b262e09141541f23bb25e4474437`
- `internal/localization/catalogs/fr.json`: `f7001552e84951ad5c9ae265839ec28c09b8683ed86874d2aeb25e1582701315`
- `internal/localization/service/fr.json`: `c2e29f1358b139e9449c4131fbc06cee1ed80bd8e3e2919bd123e05365f12bf8`

# Hausa daemon review

Reviewer: `/root/review_web_ha`. Result: **PASS** after corrections.

This is independent agent linguistic validation of every source and target entry, not native-human certification or legal-policy approval. No generated resources, English sources, other languages, runtime code, store listings, merge or release operations were changed. File-byte SHA-256 evidence is recorded in the companion JSON after corrections.

Read all 194 browser, 87 CLI and 28 service source/target pairs (309 total), including every plural branch and complete multiline help/autostart instructions. Updated the input sensor description to the configured sensitivity threshold and clarified cloud-service terminology in two browser entries. Corrected the terminal elapsed-time phrase while preserving the Go formatting order. All 28 service messages were acceptable without changes.

Verified arming versus stopping an alarm, local pairing key versus account key, suspended-phone limitations, sensor availability, location-source qualifications, update checks, language selection, numeric units and plural grammar. Kept commands, flags, diagnostic markers, paths, README, Task Scheduler, and the actual macOS System Settings → Privacy & Security → Accessibility path intact. Linux linger remains an explicit command the user must run.

Validation: exact 194/87/28 key sets, required plural fallback, named-variable multisets and ordered Go-format verbs passed for all messages. No end-to-end transport or platform execution is claimed: the previously reported browser authentication/protocol issue is outside this translation-only review.


## Gallery and Windows motion follow-up — 2026-11-02

Reviewer: `/root/review_alarm_photo_languages/review_existing_ha`. This reviewer differs from the historical `/root/review_web_ha`; the earlier report is retained. Independently reviewed and accepted the three new browser values `MOTION`, `Device moved!`, and the movement-sensor description. `MOTSI` denotes movement; the alert describes movement of the device, and the description explicitly excludes location access and raises an alarm only when movement is detected while protection is enabled. It does not describe GPS tracking, weapons, universal hardware support, or detection of every movement.

Against baseline commit `0d2dfbfe8b608557c947138bef2693b28494fc78` and the supplied snapshot, all 194 browser values are unchanged and three were added, totaling 197. All 87 CLI and 28 service values and complete file bytes match baseline. This is a three-key AI semantic delta review, not a new full-catalog/native-human/platform review. Scoped browser key/placeholder/plural structure and required review-manifest validation cover this delta; no generated bundle, runtime or release check is claimed.

Current reviewed byte hashes:
- `web/src/locales/en.json`: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- `internal/localization/catalogs/en.json`: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- `internal/localization/service/en.json`: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`
- `web/src/locales/ha.json`: `2b9b1627da7633d2e3c179aab6afc7f210366cbe8e789c1cffaa30166f78e96f`
- `internal/localization/catalogs/ha.json`: `7bf74d4c3ed6b35156247bccc3ee392ad7bd87ff8c3ba335543ac2c2f56a1b45`
- `internal/localization/service/ha.json`: `bb3398bb1bba448bafc17c733269bfd73af530495a3b697f7452fd2904563932`

Final scoped validation result: PASS for the structure/markup checks applicable to this repository and required Hausa review evidence; baseline-scope checks also passed. No shared generation or full-project validation was run.

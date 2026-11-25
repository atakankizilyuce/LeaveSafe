# Hindi daemon independent review

Result: PASS after corrections. Reviewer: `/root/review_web_hi`. Date: 2026-10-18.

Read every English/Hindi pair: 194 browser messages, 87 terminal messages and 28 autostart/service messages (309 total). This reviewer did not author those catalogs. Checked natural Hindi, protection versus alarm silencing, local pairing versus account keys, phone suspension/delivery limitations, sensors, status and elapsed-time expressions, named variables/plural forms, Go format argument order, diagnostic markers and literal command/path names.

Browser corrections: synchronized the input sensor explanation with the configured sensitivity threshold; replaced the vague translation of the laptop contact alert with its actual contact meaning; spaced and punctuated four elapsed-time abbreviations for unambiguous Hindi reading (seconds/minutes/hours/days). The two plural messages use grammatically invariant Hindi formulations suitable for all counts. CLI and service messages required no semantic edits after full review. macOS guidance preserves the actual English settings path alongside Hindi; Windows instructions describe a logon task, and Linux linger remains a command the user must run.

Validation: global `python3 tools/localization/check.py` passed. Additional Hindi-only checks confirmed exact 194/87/28 key sets, named-variable multisets for every branch, and ordered Go formatting tokens. Companion JSON hashes bind all three final source and target files.

Limitations: independent agent linguistic validation, not native-human certification or a claim that pairing/protocol behavior, platform execution, fonts or all screen layouts have been tested. Generated assets and shared code were not edited.


## Windows physical-motion delta — 2026-11-02

Independent AI reviewer: `/root/review_alarm_photo_languages/review_existing_hi`. This follow-up reviewer differs from the historical `/root/review_web_hi`; the original full review above is preserved. Read all three new English/Hindi browser messages in local daemon sensor context, rather than treating shared app wording as evidence. Baseline commit: `0d2dfbfe8b608557c947138bef2693b28494fc78`.

Retained `MOTION` as “हलचल” and `Device moved!` as “डिवाइस हिला!”. Corrected the description's awkward “हलचल मिलने” to “हलचल का पता चलने”: the sensor monitors physical movement without location access, and detection triggers an alarm while protection is enabled. Protection is not a weapon or OS lock, and the text makes no GPS, cloud, universal hardware, macOS support, or every-movement guarantee.

Read-only checks passed: 194 prior browser values exact + three additions = 197; complete source/target keys and named-variable/plural contracts. All 87 CLI and 28 service values match the snapshot, and both target files are byte-identical to the baseline commit; their Go-format checks pass. Manifest hashes include all three current English and Hindi files. Scoped `reviews.validate(root=root, locales={'hi'}, require_all=True)` passed. This is a three-message semantic delta, not a fresh review of all unchanged catalogs, native-human certification, hardware testing or release approval. No CLI/service/shared/generated assets were edited.

Browser English SHA-256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Browser Hindi SHA-256: `34d6193d1b7ac64bebb2f4db2d1bab526b356312d01d4e7f98da7d819a595282`.

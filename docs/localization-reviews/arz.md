# Egyptian Arabic browser and terminal review — PASS

Reviewer: `/root/review_web_arz` (independent of the translation authors).

This is agent linguistic validation, not native-human certification, legal-policy approval, or a claim of physical-device testing. English source and Egyptian Arabic target were read in full, not sampled. Exact-byte hashes in the companion JSON identify the reviewed versions.

Scope: all 194 browser entries (including every plural branch), 87 terminal messages including full help, and 28 platform-service messages. Reviewed monitoring/arming versus stopping or dismissing an alarm, local pairing keys versus cloud accounts, notification limitations, sensors, location providers, units, numbers, technical identifiers, commands and paths.

Corrected the browser input-sensor explanation to the configured sensitivity threshold. Improved the CLI actual-path-resolution failure wording. Included exact English OS menu names alongside Arabic explanations for Task Scheduler and macOS System Settings → Privacy & Security → Accessibility, so the instructions do not imply an Egyptian Arabic OS interface exists. Windows remains a logon task; Linux boot/logout behavior remains conditional on the displayed command. Existing Egyptian Arabic plural/count formulations and time units preserve all variables.

Validation: exact 194/87/28 key sets, named-variable multisets for every plural branch, and ordered Go format arguments passed. Commands, flags, configuration identifiers and diagnostic markers remain literal. No generated resources or protocol code edited. This review does not validate the pre-existing browser/server pairing-protocol compatibility; integration and platform runtime verification remain with the integration owner. No unresolved translation issue identified.

## Physical movement additions — 2026-11-02

Independent follow-up reviewer: `/root/review_alarm_photo_languages/review_existing_arz`, separate from the initial draft author. This is the actual reviewer for this delta, different from the historical reviewer above; that earlier report is preserved. Reviewed only the three new browser messages `MOTION`, `Device moved!`, and `Watches physical movement without location access. Triggers when movement is detected while armed.` against current English and the daemon browser context. Baseline commit `0d2dfbfe8b608557c947138bef2693b28494fc78` and `localization-baseline.json`. This does not claim a new full browser/terminal/service semantic review.

All three Egyptian Arabic drafts are accepted without correction. الحركة denotes the motion sensor; الجهاز اتحرّك! reports physical device movement. The explanation explicitly excludes location access and says detected movement triggers an alarm while protection is active. It does not confuse arming with a weapon or an OS lock, imply GPS tracking, guarantee every movement will be detected, promise hardware support on every computer, or introduce mobile/cloud behavior into the local daemon panel.

Scoped validation passed: **194 prior browser values exactly unchanged + three new messages = 197**; the **87 CLI and 28 service entries remain byte-identical to the baseline**, and their objects match the snapshot. Browser named-variable/plural structure checks, unchanged CLI/service placeholder and ordered Go-format checks, and `reviews.validate(root=Path.cwd(), locales={'arz'}, require_all=True)` passed. No generated assets, CLI/service translations, application code or other locales were changed. No native-human certification, physical-device verification, layout/accessibility, legal-policy or store approval is claimed.

Current source SHA256:

- `web/src/locales/en.json`: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- `internal/localization/catalogs/en.json`: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- `internal/localization/service/en.json`: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`

Current reviewed-file SHA256:

- `web/src/locales/arz.json`: `8f45afc6246b8c7a75f46e79f54d56899b030da870b1b5a6d7808a3907bcfafe`
- `internal/localization/catalogs/arz.json`: `87c9428b0896f82f36f12fc6ece9666267eb7a44c16e59f764403e48875b2e38`
- `internal/localization/service/arz.json`: `eb45a5d392b89ee89a962a75199274a10211cd4d786dfec3728b83a2e2c5a18c`

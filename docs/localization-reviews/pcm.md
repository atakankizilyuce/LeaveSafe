# Nigerian Pidgin daemon review — PASS

Read and compared all 194 browser, 87 terminal and 28 platform-service entries (309 total), including every plural branch and complete multiline help message.

Corrected the input-sensor explanation to say that configured sensitivity must be reached while protection is on, keeping the source JSON key unchanged. Refined lockout attempt count, progress/completion, unset-key and alarm-dismissal wording. Alarm dismissal means making the alarm quiet; disarm remains stopping protection. Improved terminal monitoring-gap, window-bottom and current-language instructions. Reviewed the 28 service messages without changes: Windows Task Scheduler remains a user-logon task; macOS retains the actual English System Settings → Privacy & Security → Accessibility labels; Linux startup after logout/boot remains conditional on the shown enable-linger command.

Preserved local pairing-key meaning, PIN requirements, optional location/provider calls, browser suspension limits, executable commands, paths, flags, diagnostic markers, ANSI placeholder positions, numbers and units.

Validation: global catalog checker passed; pcm exact key sets, named-variable multiplicities and ordered Go-format tokens were independently checked. No unresolved translation issue found. This review does not certify the existing browser/server authentication protocol or end-to-end connectivity.

Reviewer: `/root/review_web_pcm`, independent of the translation authors.
Date: 2026-10-18. Language: Nigerian Pidgin (`pcm`).

This is independent agent linguistic validation. It is not native-human certification, legal-policy approval, a runtime security audit, or proof of end-to-end pairing/notification delivery. Shared generation, fonts, UI rendering, platform builds, CI and Sonar checks remain the integration owner's responsibility.


## Gallery and Windows motion follow-up — 2026-11-02

Reviewer: `/root/review_alarm_photo_languages/review_existing_pcm`, independent of these draft translations. Historical review text and former reviewer identities remain preserved. This is a focused AI semantic delta review, not a fresh full-catalog review, native-human certification, device test, legal-policy approval or store acceptance.

Reviewed only the three new browser messages independently in daemon context. Accepted `MOVEMENT` and `Device don move!` as the physical-motion label and completed movement event. Corrected the description to natural ongoing monitoring and an explicit alarm trigger when movement is detected while protection is on. It excludes location access, does not promise every motion will be detected, and does not confuse protection with weapons. No cloud or universal hardware support claim was added.

The current reviewer differs from the historical `/root/review_web_pcm`; that earlier full review remains historical. Baseline `0d2dfbfe8b608557c947138bef2693b28494fc78` and supplied snapshot preserve all 194 old browser values; 3 added = 197. All 87 CLI and 28 service entries are unchanged, with file bytes equal to baseline and previous hashes. Scoped browser/CLI/service structure, named-variable and ordered Go-format validation plus `reviews.validate(root, locales={'pcm'}, require_all=True)` passed. No new semantic review of unchanged CLI/service catalogs is claimed.

Current file-byte SHA256 values:
- `web/src/locales/en.json`: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- `internal/localization/catalogs/en.json`: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- `internal/localization/service/en.json`: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`
- `web/src/locales/pcm.json`: `7ae7f565be34885673289d22b4504bbd8945e6dcedba561cbc39de00692f33b8`
- `internal/localization/catalogs/pcm.json`: `55ba094d4160c2582e7bf90b886c3aeef1d1495b190f999dbfcd1013fb83fdcb`
- `internal/localization/service/pcm.json`: `72156ac7a1a466ac6847341a150b51a86fbd8db02645136c88f4ca0ed5f576e9`

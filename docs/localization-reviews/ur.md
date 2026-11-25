# Urdu independent browser and terminal review

PASS — 2026-10-18. Reviewer: `/root/review_web_ur`.

Read every English and Urdu entry: browser 194, terminal 87, service/autostart 28 (309 total). Independent of authorship. Corrected three browser entries: input triggers at the configured sensitivity threshold rather than any input; watching sensitivity rather than only polling frequency; neutral touch detection without asserting intentional tampering. Improved the long CLI help's wording for the development interface and headless terminal interface (one complete entry). The 28 service messages required no corrections.

Checked arming versus stopping monitoring and silencing an alarm, local pairing keys versus account credentials, suspended-phone notification limits, source and distance units, plural branches, permissions, platform availability, Windows logon task, Linux explicit linger command and boot/logout conditions. macOS System Settings → Privacy & Security → Accessibility, Task Scheduler, commands, flags and technical identifiers remain literal where necessary. Exact key sets (194/87/28), named-variable multisets across all plural branches and ordered Go format verbs passed read-only assertions.

Limitations: agent linguistic validation, not native-human certification or platform/E2E testing. The existing browser authentication/protocol limitation remains an integration matter; this review does not certify that pairing works end to end. RTL rendering and real device behavior require integration validation. No shared code or generated assets changed.


## Physical motion browser delta — 2026-11-02

Reviewer: `/root/review_alarm_photo_languages/review_existing_ur`, independent of the submitted drafts. This is a new follow-up reviewer identity, distinct from historical reviewer `/root/review_web_ur`; the earlier report remains historical. Reviewed only the three added browser messages against frozen English and their daemon sensor context, not a fresh semantic review of the complete browser/CLI/service catalogs.

All three drafts are acceptable without correction. `MOTION` names movement; `Device moved!` reports physical device movement; the explanation explicitly excludes location access and makes alarm triggering conditional on movement being detected while protection is active. Protection does not refer to a weapon, and the text promises neither GPS tracking nor detection of every movement. No mobile cloud behavior or unsupported platform promise was introduced.

Against snapshot baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`, all 194 old browser values remain exact and three additions produce 197. The 87 CLI and 28 service values match the snapshot and their files are byte-for-byte identical to the baseline commit; no new semantic review of these unchanged entries is claimed. Duplicate-key JSON, scoped browser/terminal/service structural and placeholder checks, ordered Go-format checks and `reviews.validate(root, locales={'ur'}, require_all=True)` passed. The manifest records all three true English and target file hashes. No generation, E2E/native-device/RTL acceptance, release or human-language certification is claimed.

Browser English SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Browser Urdu SHA256: `bdd1b7b59e332b06d692c1efbddd23d5127ed2175dab786fda99c62776d26296`.

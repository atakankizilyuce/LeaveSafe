# Western Punjabi (Shahmukhi) daemon independent review

Reviewer: `/root/review_web_pnb`  
Date: 2026-10-18  
Result: PASS for the source and target byte hashes in the companion manifest.

Read every source and target message: 194 browser entries (including every plural branch), 87 terminal entries and 28 platform service entries, 309 in total. The translation author is a different agent.

Corrected four browser entries: input fires at the configured sensitivity threshold, phone disconnection rather than physical phone breakage, the breadth of monitoring sensitivity rather than only its polling frequency, and a spelling error. Corrected two terminal entries: the ordering of the armed-since phrase and explicit retention of the existing pairing key rather than suggesting a newly placed key. All 28 service messages required no change. Retained macOS's actual English System Settings → Privacy & Security → Accessibility path, Windows Task Scheduler and the Linux linger command and its required action. Checked local pairing versus account keys, disarm versus silencing, suspended-phone limitations, named variables, complete Punjabi plural forms, units, exact Go formatting order, literal CLI flags and paths.

Validation: exact 194/87/28 key sets and named-variable multisets / ordered Go format verbs passed. No shared resources were generated.

This is independent agent linguistic validation, not native-human certification or end-to-end authentication, OS permission or transport verification. The pre-existing browser authentication compatibility concern is outside this language review. Rendering and RTL interaction need integration checks. Source or target changes invalidate this evidence.


## Physical motion browser delta — 2026-11-02

Independent follow-up reviewer: `/root/review_alarm_photo_languages/review_existing_pnb_arab`, a different identity from the historical daemon reviewer recorded above. Earlier evidence is preserved. This focused AI review covers the three new browser messages, not a new review of all browser/terminal/service text or native-speaker/device certification.

Reviewed `MOTION`, `Device moved!`, and the physical-movement sensor explanation independently in the daemon's local-protection context. Accepted the movement label and device-moved event. Refined the explanation from “real movement” to the device physically moving, with no location access. Its alarm condition requires protection to be enabled and movement to be detected; it does not promise detection of every movement, GPS tracking, cloud delivery, or universal platform support. Protection is not a weapon or OS lock.

Baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`: all 194 browser values remain exact, with only three additions (197 total). CLI 87 and service 28 values match the snapshot and the files are byte-identical to the baseline commit. No terminal/service semantic re-review is claimed. Scoped duplicate-key-safe parsing, browser keys/plural/placeholder contracts, terminal format checks, frozen browser source hash, and `reviews.validate(locales={'pnb-Arab'}, require_all=True)` passed. No shared generation or physical-device/UI testing was performed.

Current source hashes:

- `web/src/locales/en.json`: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- `internal/localization/catalogs/en.json`: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- `internal/localization/service/en.json`: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`

Current reviewed-file hashes:

- `web/src/locales/pnb-Arab.json`: `1bd111b67bf5abee3789607c2e6cce6f76edb62cf22d6a3b74990b0bfa2c8dae`
- `internal/localization/catalogs/pnb-Arab.json`: `f8233ded0ec0d560e59b9a03655c1a52a27aab8ccd257550a3f57cb0cb9e3a18`
- `internal/localization/service/pnb-Arab.json`: `4fa2c09ab15c45d295efd13e4f5dd55823ec9f5c8e00a799cf0708ff4a51d2d7`

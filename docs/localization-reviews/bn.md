# Bengali independent review — PASS

Reviewer: `/root/review_web_bn` (independent of the translation author).
Date: 2026-10-18.

Read every English and Bengali message across the browser panel (194), terminal catalog (87) and platform autostart instructions (28): 309 entries. This is the Bengali review in a five-surface review of 968 entries; existing app/core and backend reviews were not changed.

Corrected six browser values: synchronized the approved input-sensor sensitivity threshold wording, restored the physical-touch meaning of the alert headline, and spaced four time expressions for readability (including an unambiguous Bengali word for days). Retained the original input-sensor JSON key. Terminal and service messages needed no semantic corrections.

Checked protection/arming versus disarming and silencing, local pairing keys versus account keys, PIN prompts, connection loss and suspended-phone delivery limits, optional geolocation and GitHub checks, sensor causes, Bengali count phrasing, technical identifiers, CLI flags and commands, line layout and ordered Go format arguments. Windows remains a logon task, macOS retains the actual English System Settings → Privacy & Security → Accessibility names with Bengali explanatory gloss, and Linux linger remains conditional on the displayed command.

Validation: all three exact key sets and counts, exact named-placeholder multiplicities in every plural branch, and ordered Go formatting passed. The shared read-only catalog checker also passed. Companion manifest hashes bind this review to the final source and target bytes.

Limitations: independent agent linguistic validation, not native-human certification. No claim of physical-device, browser authentication or end-to-end pairing validation; the separately reported browser protocol issue remains outside this translation review. Shared generation, builds and CI belong to the integration owner.


## Desktop motion: three-message delta — 2026-11-02

Independent reviewer: `/root/review_alarm_photo_languages/review_existing_bn`, separate from the draft author. This follow-up reviewer differs from the historical reviewer above; earlier evidence is preserved rather than attributed to this agent. Reviewed the three new browser messages and nearby sensor context against frozen English and the translation contract. No corrections were needed.

`MOTION` is natural physical-movement terminology; `Device moved!` identifies movement of the monitored device. The explanatory sentence explicitly excludes location access and triggers only when movement is detected while protection is enabled. It does not imply GPS, weapons, every-movement detection, a cloud/mobile service, or unsupported-platform availability. App-reused wording was checked independently for the daemon browser context.

Against snapshot/baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`, all 194 previous browser values are exact, with three additions for 197 total. All 87 CLI and 28 service entries match the snapshot and both files are byte-identical to the baseline commit. All three current source/target file hashes are recorded in the companion manifest. Scoped browser key/placeholder/plural checks, unchanged terminal Go-format checks, and `reviews.validate(locales={'bn'}, require_all=True)` passed. This is a three-message AI linguistic delta review, not a fresh complete-catalog or native-human/device certification; shared generation, build and CI remain with integration.

Browser English SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`. Browser Bengali SHA256: `a7cfdfad5296223c8f9136643ad9feae87a974204e37c9e981c303dafc942246`.

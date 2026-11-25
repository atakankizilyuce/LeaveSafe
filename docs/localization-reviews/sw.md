# Swahili independent daemon review

Reviewer: `/root/review_web_sw`  
Date: 2026-10-18  
Result: PASS

Independently read every English and Swahili message in the browser catalog (194), terminal catalog (87) and platform service catalog (28), including both plural branches. The catalogs were authored by another agent.

Corrected the input-sensor explanation to retain the configured sensitivity threshold while protection is armed, corrected the translation of coordinates, and made the dynamic sensor-unavailable sentence grammatically independent of the sensor label. Improved the development-build terminal message. No service-catalog correction was necessary.

Verified distinctions between arming, disarming and silencing; local pairing keys versus cloud account keys; phone suspension and connection limits; optional location requests and update checks; units and singular/plural forms; unchanged CLI commands, configuration identifiers, Go formatting order and named variable multisets. The service instructions preserve real macOS English menu labels with Swahili explanations, the Windows logon task, and the explicit Linux linger command requirement.

Read-only validation passed for exact 194/87/28 key sets, all named variable multisets and all Go format sequences. Exact source and target byte hashes are recorded in the companion manifest.

This is agent linguistic validation, not native-human certification or a claim that browser pairing works end to end. The known browser/server authentication compatibility issue is outside this language review. Platform execution, shared generation and CI/security gates remain integration responsibilities.


## Physical-motion browser delta review — 2026-11-02

Reviewer: `/root/review_alarm_photo_languages/review_existing_sw`, a different follow-up reviewer from the historical `/root/review_web_sw`; the original report is retained. Independently reviewed only the three new browser values against English and the daemon browser context. `MWENDO`, the device-moved alarm, and the explanatory text faithfully express physical device movement, no access to location information and an alarm when movement is detected while protection is enabled. No correction was necessary; no GPS tracking, weapon meaning, remote-camera or cloud-delivery behavior is introduced. This is a focused AI semantic review, not another full-catalog reading or native-human/runtime certification.

Against baseline `0d2dfbfe8b608557c947138bef2693b28494fc78` and the frozen snapshot, all 194 prior browser values are unchanged, plus three additions = 197. The 87 CLI and 28 service catalogs are byte-identical to that baseline and match the snapshot; their historical reviews are retained rather than claimed as fresh semantic reviews. Scoped browser/CLI/service key and placeholder validation, Go-format validation and `reviews.validate(locales={'sw'}, require_all=True)` passed. The manifest records actual hashes for all three source and target files. No shared assets, runtime tests or integration gates are included.

Browser English SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Browser Swahili SHA256: `01685a90f9c4c5e8cb16e4527855f9989afe7cb8378fd65208be8209c9c95012`.

# Marathi daemon independent review

- Result: PASS after corrections, 2026-10-18.
- Reviewer: `/root/review_web_mr`, independently assigned only Marathi, separate from its author.
- Scope: all 194 browser, 87 CLI and 28 service messages (309 total), compared with the complete English source.

Updated the browser input sensor explanation to the configured sensitivity threshold without changing its source key. Corrected the phone connection sentence's grammar and standardized the screen sensor label with the surrounding Marathi wording. The remaining browser messages preserve arming versus dismissing the alarm, local pairing keys, PIN checks, connection loss, optional location/update behavior, suspended browser delivery limits and sensor availability. Marathi count phrases are grammatical across the supported `other` plural branch and retain all variables.

All CLI and service messages were read and retained: commands, flags, file names, diagnostic markers, Go formatting order and split multiline paragraphs are correct. Windows describes a scheduled logon task, Linux linger remains an action the user must execute, and macOS retains the actual English System Settings → Privacy & Security → Accessibility names with a Marathi explanation. No cloud account operations were implied.

Read-only validation: global `tools/localization/check.py` passed; additionally verified exact Marathi key counts/sets, named-variable multisets in every plural branch and all Go formatting verbs in argument order.

This is independent agent linguistic validation, not native-human certification or legal-policy approval. Runtime/visual platform testing and CI are the integration owner's responsibility. This review does not resolve the separate daemon browser authentication compatibility issue. No English source, other language, shared code, generated output, store, release or Git changes were made by this reviewer. Byte hashes below bind the reviewed content.


## Physical motion browser delta — 2026-11-02

Independent follow-up reviewer: `/root/review_alarm_photo_languages/review_existing_mr`, different from the historical reviewer identified above. Historical review text is preserved. This is a focused AI semantic review of three additions, not a fresh full-catalog review, native-human certification, or hardware/platform test.

Independently reviewed `MOTION`, `Device moved!`, and `Watches physical movement without location access. Triggers when movement is detected while armed.` against current English in the daemon browser context. All three Marathi drafts are acceptable without correction. “हालचाल” and “डिव्हाइस हलले!” describe physical device movement. The explanation explicitly excludes location access, conditions the alarm on detected motion while protection is active, and uses protection terminology rather than weapon terminology. It adds no GPS dependency, cloud delivery, universal Windows hardware availability, native Mac support, or guarantee that every movement is detected.

Snapshot baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`: all 194 prior browser values remain exact, with three additions giving 197. CLI 87 and service 28 catalogs match snapshot values and baseline file bytes exactly; they were not edited or newly linguistically certified. Source and target hashes for all three catalog groups are recorded in the manifest.

Scoped browser key/placeholder/plural validation, CLI/service structure and Go-format preservation checks, and `reviews.validate(locales={'mr'}, require_all=True)` passed. No shared generation or runtime verification was performed.

Browser English source SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Browser Marathi SHA256: `f161da1769b504a490db3318bdd21a99efe963a7fd11915b37b6b1dde738dae8`.

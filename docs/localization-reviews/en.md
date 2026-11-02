# English daemon independent review

Reviewer: `/root/review_web_en`  
Date: 2026-10-18  
Result: PASS for the reviewed language content; functional limitation noted below.

Read all 194 browser messages, all 87 terminal messages and all 28 platform-service messages. This reviewer did not author the initial English catalogs. Reviewed arming versus disarming, alarm dismissal versus stopping monitoring, local pairing keys versus cloud-account credentials, PIN requirements, the phone sleep/disconnection limitation, sensor states and events, settings and location sources, update behavior, singular/plural branches, time/distance units and failure/retry messages.

One source error was reported to the integration owner and corrected with explicit approval: the input-sensor explanation previously promised an alert on any input, despite the configurable sensitivity threshold. The new value explains that the input must reach that threshold while armed. The existing source key is deliberately unchanged for lookup compatibility. Checked this against the monitor input-activity logic and the configured sensor construction. Reread the final value in its sensor-help context.

The terminal review checked all Go formatting verbs and their argument order, color/reset placeholders, command and configuration identifiers, multiline help text, stored-key warnings and language selection. The service review checked macOS LaunchAgent and Accessibility instructions, Windows logon Scheduled Task terminology, and Linux systemd/linger instructions without implying boot or post-logout behavior is enabled automatically. No CLI/service wording changes were needed.

Validation: all three English catalogs pass the structural message validator; terminal/service format sequences match their source keys. The exact-file review manifest validates for English. No other language, generated browser build or shared assets were edited.

This independent AI language review does not certify native-human review or full platform execution. In particular, the separately tracked browser raw-key/v2-server pairing incompatibility still prevents claiming successful browser pairing and end-to-end notifications; it must be resolved before presenting that workflow as verified. Unknown diagnostic details can remain technical server text. The companion JSON binds the report to the final browser, terminal and service source bytes.


## Windows motion browser delta — 2026-11-02

Reviewer: `/root/review_alarm_photo_languages/review_existing_en`, independently reviewing the coordinator's three-message English draft. This focused semantic review covers only `MOTION`, `Device moved!`, and `Watches physical movement without location access. Triggers when movement is detected while armed.` Earlier full-catalog review, identity, scope and limitations above remain historical evidence; this follow-up does not claim a fresh review of all 197 browser, 87 terminal and 28 service messages.

No wording corrections were needed. The label names physical motion, the alert describes a detected device movement, and the explanation expressly excludes location access and makes triggering conditional on detection while armed. None promises detection of every possible movement, system locking, or macOS support. The supplied feature contract limits the new implementation to supported Windows hardware. The browser sensor/help and alert-copy mappings reference these messages. No variables, formatting verbs or plural branches were added.

Exact comparison with `localization-baseline.json`, daemon source commit `0d2dfbfe8b608557c947138bef2693b28494fc78`, passed: all 194 prior browser messages remain identical, with exactly three additions for a total of 197; all 87 terminal and 28 service messages remain identical. Their existing review provenance is retained, and their unchanged file hashes are included because the evidence schema requires all three surfaces. English duplicate-key parsing, scoped browser/terminal/service structural validation, unchanged terminal/service Go-format validation, and `reviews.validate(locales=['en'], require_all=True)` passed.

This is an AI language review of the three-message delta, not native-speaker certification, hardware validation, a new review of unchanged terminal/service wording, or confirmation of the historical browser integration limitation's current status. Native Windows sensors, detection thresholds, physical movement, display layout, complete multi-language gates and CI were not exercised here. Only this English review evidence was edited by this reviewer; source wording was accepted unchanged.

Current file-byte SHA256 evidence:

- Browser: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- Terminal: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- Service: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`

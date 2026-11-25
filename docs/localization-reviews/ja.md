# Japanese daemon review

Reviewer: `/root/review_web_ja`
Result: PASS after correction

Read all 194 browser, 87 terminal and 28 platform-service entries against English (309 entries). Corrected the browser input-sensor hint to say that input triggers at the configured sensitivity threshold while armed, retaining the original JSON key. The other browser strings preserve local pairing versus account keys, monitoring versus OS locking, alarm dismissal versus disarming, connection/sleep delivery limits, optional location sources, update checks, units and variables. Japanese plural objects correctly use `other`.

All 87 terminal and 28 service messages were acceptable without edits. Verified command names, flags, diagnostic tags, filenames, Go formatting argument order, color markers, the two-line persisted-key explanation and full help text. macOS uses システム設定 → プライバシーとセキュリティ → アクセシビリティ; Windows describes the logon task in タスク スケジューラ; Linux linger instructions require the displayed command.

Validation: global `python3 tools/localization/check.py` passed; all three Japanese key sets match English exactly, and terminal/service format verbs match in order. Companion hashes cover the final files. No unresolved Japanese meaning defect remains. This review does not claim the known browser authentication-transport limitation is fixed or that pairing succeeds end to end.

This is an independent agent linguistic review of Japanese against the complete current English sources, not native-human certification or legal-policy approval. It checks semantic fidelity, grammar, product context, numbers, limitations, and preserved technical tokens. It does not establish CI, security, coverage, rendered layout or real-device behavior. Those integration checks remain with the root agent. No other language, English source, generated resources, existing core-app review, merge, release or store material was changed.


## Windows physical-motion additions — 2026-11-02

Independent delta reviewer: `/root/review_alarm_photo_languages/review_existing_ja`, the existing Japanese app follow-up reviewer. This identity differs from the historical daemon reviewer `/root/review_web_ja`; the earlier complete-catalog review remains above and is not represented as work repeated here. Baseline: `localization-baseline.json`, daemon commit `0d2dfbfe8b608557c947138bef2693b28494fc78`.

Independently reviewed only the three new browser messages against the frozen English source and browser translation contract: `MOTION`, `Device moved!`, and `Watches physical movement without location access. Triggers when movement is detected while armed.` All three drafts are accepted without corrections. `動き` is a natural compact physical-motion sensor label; `デバイスが動きました!` reports device movement; the longer hint explicitly says physical movement is watched without location access and that detection while protection is active triggers the alarm. `保護中` refers to enabled protection, not weapons or operating-system locking. These statements neither promise detection of every movement nor imply GPS tracking, cloud delivery, or macOS sensor availability.

Preservation checks confirm **194 prior browser values unchanged + 3 additions = 197**. All **87 CLI** and **28 service** values match the baseline snapshot, and both target files are also byte-identical to the recorded baseline commit. Scoped `check.validate` passed for all three Japanese catalogs, terminal/service format checks passed, and `reviews.validate(locales={'ja'}, require_all=True)` passed. The evidence manifest records the true current three English source hashes and all three target hashes. CLI/service preservation is not a new linguistic review of those historical entries.

Only Japanese review documentation/evidence were edited in this repo; the accepted browser drafts were left unchanged. No CLI/service, code, shared generation, assets, commits, or publication were changed. This focused AI delta review does not claim native-human certification, full-catalog re-review, physical-device/OS acceptance, or font/layout verification.

Reviewed browser English SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Reviewed `web/src/locales/ja.json` SHA256: `321389cba67828b44f580f6e5fa133684f49702f903f816650943a94e7658330`.
Unchanged CLI SHA256: `5ef072fa29c85da00eb17e29c33e89e87d3cd0721cc51f3ccc962fc2d71c02d6`.
Unchanged service SHA256: `bf25e43626bb52725b0ecd948852c0927fdf917bc99b6e99127564d1bfde4ddd`.

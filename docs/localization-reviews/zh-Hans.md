# Simplified Chinese independent linguistic review

Reviewer: `/root/review_web_zh`  
Date: 2026-10-18  
Result: **PASS** after correction.

Read and compared every English source and Simplified Chinese target message: 194 browser entries (including every plural branch), 87 terminal entries and 28 platform service entries. This reviewer did not author these catalogs. The companion manifest binds this review to the exact final source and target bytes.

Corrected the browser keyboard/trackpad explanation to say input must reach the configured sensitivity threshold while armed; the lookup key remains unchanged. The remaining browser messages and every terminal/service message preserve the practical meaning without further correction: starting/stopping protection versus dismissing an alarm, local pairing key versus cloud account credentials, phone suspension and conditional alerts, optional location/network use, units, and update behavior. Chinese uses the `other` plural branch naturally. Named variables, ordered Go format arguments, diagnostic markers, literal commands/configuration fields, Windows logon-task meaning, macOS 系统设置 → 隐私与安全性 → 辅助功能 labels and Linux linger prerequisites were checked.

Validation: website/installer locale validation passed separately; daemon check.py passed across all 25 catalogs. Exact 194/87/28 key sets and ordered Go formatting tokens were independently checked for this language.

This is full agent linguistic validation, not native-human certification, security approval or a platform runtime/pairing test. No claim is made that the pre-existing browser transport mismatch is fixed. No generated resources, other locales, application-core reviews, Git operations or store content were changed.


## Physical motion browser delta — 2026-11-02

Independent delta reviewer: `/root/review_alarm_photo_languages/review_existing_zh_hans`. This reviewer differs from historical reviewer `/root/review_web_zh`; the original full-review report above remains historical evidence. Independently reviewed the coordinator-authored three-message draft against the frozen English browser source, surrounding Chinese sensor/status text, `tools/localization/TRANSLATING.md`, and the supplied motion behavior contract. This is a focused AI language review, not a new full-catalog audit, native-human certification or physical-device acceptance.

Reviewed all three additions: `MOTION`, `Device moved!`, and `Watches physical movement without location access. Triggers when movement is detected while armed.` The label 移动 appropriately names physical movement in this sensor group. The event 设备发生了移动！ describes device movement rather than GPS relocation; corrected its ASCII exclamation mark to Chinese punctuation. In the explanation, replaced 无需访问位置 with 不访问位置信息 to state explicitly that this sensor does not access location, rather than merely saying location access is unnecessary. The armed condition is naturally expressed as 防护开启时 and the trigger remains conditional on detected movement. The text adds no every-movement guarantee, GPS measurement, macOS support or cloud-delivery promise. The reused app label was checked independently in the daemon's local sensor context.

Baseline commit: `0d2dfbfe8b608557c947138bef2693b28494fc78`; comparison snapshot: `output/leavesafe-gallery-motion/localization-baseline.json`. All **194 prior browser values are unchanged**, with exactly **3 additions = 197 messages**. The **87 CLI and 28 service messages** match the snapshot and their files are byte-identical to the baseline commit; they were not edited or freshly re-audited. The manifest keeps their real current source and target hashes alongside the reviewed browser hashes.

Validation: focused browser `check.validate` passes exact key coverage, nonempty values, plural-object structure and named placeholders. Scoped CLI/service structural and Go-format checks also pass without edits. `reviews.validate(root, locales={'zh-Hans'}, require_all=True)` passes. Only this browser catalog and its own report/manifest were edited in the daemon repo. No generation, commit, push, runtime sensor/hardware test, layout/font check or release/store certification was performed.

Reviewed browser English SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Reviewed `web/src/locales/zh-Hans.json` SHA256: `7592e034f3ee69d84d3517bf86be0c40a9b0825ecc85a663b2c62dafc6854b57`.
Unchanged CLI SHA256: `a7f9a5e39d47ea075be8a43ac72af2453d4a2a49467688826a949bbabb4769e0`.
Unchanged service SHA256: `74e1c86475b0ef670d36561a59ae009a65a9c7df1730e03b2ca478aa6cab7e1d`.

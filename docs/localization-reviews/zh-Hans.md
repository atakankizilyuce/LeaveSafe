# Simplified Chinese independent linguistic review

Reviewer: `/root/review_web_zh`  
Date: 2026-10-18  
Result: **PASS** after correction.

Read and compared every English source and Simplified Chinese target message: 194 browser entries (including every plural branch), 87 terminal entries and 28 platform service entries. This reviewer did not author these catalogs. The companion manifest binds this review to the exact final source and target bytes.

Corrected the browser keyboard/trackpad explanation to say input must reach the configured sensitivity threshold while armed; the lookup key remains unchanged. The remaining browser messages and every terminal/service message preserve the practical meaning without further correction: starting/stopping protection versus dismissing an alarm, local pairing key versus cloud account credentials, phone suspension and conditional alerts, optional location/network use, units, and update behavior. Chinese uses the `other` plural branch naturally. Named variables, ordered Go format arguments, diagnostic markers, literal commands/configuration fields, Windows logon-task meaning, macOS 系统设置 → 隐私与安全性 → 辅助功能 labels and Linux linger prerequisites were checked.

Validation: website/installer locale validation passed separately; daemon check.py passed across all 25 catalogs. Exact 194/87/28 key sets and ordered Go formatting tokens were independently checked for this language.

This is full agent linguistic validation, not native-human certification, security approval or a platform runtime/pairing test. No claim is made that the pre-existing browser transport mismatch is fixed. No generated resources, other locales, application-core reviews, Git operations or store content were changed.

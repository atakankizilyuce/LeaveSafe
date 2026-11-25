# Arabic daemon independent review

Reviewer: `/root/review_web_ar`  
Date: 2026-10-18  
Result: PASS for Arabic language fidelity after corrections.

Read all 194 browser messages (including every Arabic plural branch), all 87 terminal entries including the complete help text, and all 28 platform-service messages against English. This reviewer did not author these translations.

Corrected the browser input-sensor explanation to say that input must reach the configured sensitivity threshold while monitoring is armed; the lookup key remains unchanged. Corrected three terminal formulations: grammatical agreement for restoring armed state, the number of clients receiving a test, and a count-neutral connected-client label. The service catalog required no edit.

Checked the distinction between enabling/stopping monitoring and silencing an alarm, local pairing key versus account key, PIN requirements, sleeping-phone delivery limitations, location opt-ins, updates, sensor events, Arabic plural categories, units and durations. Checked every CLI Go-format argument in order, including color arguments, literal commands, flags, config keys and paths. Checked Windows logon tasks, macOS LaunchAgent permissions and Linux linger prerequisites. Arabic macOS labels match [Apple’s Arabic Privacy & Security documentation](https://support.apple.com/ar-qa/guide/mac-help/-mchl211c911f/mac): إعدادات النظام، الخصوصية والأمن، تسهيلات الاستخدام. Commands and tool names remain literal.

Validation: exact 194/87/28 key sets, exact named-variable multisets for all plural branches, and ordered Go-format sequences passed; `python3 tools/localization/check.py` also passed. The companion manifest hashes all three English and all three Arabic catalogs.

This is an independent AI language review, not native-human certification or full platform execution. The separately tracked browser authentication incompatibility with the v2 server remains a functional integration issue; this report does not claim successful browser pairing or notification end-to-end tests. RTL terminal rendering depends on terminal support. No shared code or generated assets were changed.


## Physical-motion browser delta review — 2026-11-02

Independent reviewer: `/root/review_alarm_photo_languages/review_existing_ar`. This reviewer is different from the historical `/root/review_web_ar`; the earlier whole-catalog report remains preserved evidence. Independently reviewed the three separately authored new Arabic browser messages against current English and nearby device/sensor text. Baseline commit: `0d2dfbfe8b608557c947138bef2693b28494fc78`; snapshot: `output/leavesafe-gallery-motion/localization-baseline.json`. This is only a three-message semantic delta, not a new full-catalog review or native-human certification.

`MOTION` (الحركة) is an appropriate physical-motion sensor label, and `Device moved!` (تحرّك الجهاز!) correctly describes the device moving. Corrected the explanatory message to say explicitly that triggering occurs **when protection is active**, avoiding the possible reading of the draft as only during the activation action. It describes physical movement without location access, with an alarm upon detected movement under active protection. It implies neither GPS tracking nor a weapon, remote cloud monitoring, universal hardware support, macOS support or guaranteed detection of every movement. The app-derived terminology was independently checked in this daemon context.

Preservation: all **194 prior browser values unchanged + 3 additions = 197 browser messages**. All 87 CLI and 28 service values match the snapshot, and both target files are byte-identical to the baseline commit. These unchanged terminal catalogs were verified for preservation and format only; their semantics are not recertified here.

Validation: PASS for exact browser addition/preservation sets; all three Arabic catalog coverage/placeholder dictionaries through `check.validate`; unchanged CLI/service Go-format contracts; frozen English browser hash; and `reviews.validate(root=root, locales={'ar'}, require_all=True)`. The manifest records all three actual English source and Arabic target file hashes. No shared code, generated files, CLI/service values or other languages were edited. No physical hardware, browser/RTL layout, terminal rendering, sensor execution or end-to-end alarm test is claimed. No unresolved semantic finding remains within these three additions.

Reviewed English browser SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Reviewed Arabic browser SHA256: `2ed068a4b33562586a6a666d1d78d3b5bb34efda54baccd130ea6d1cbeb441cd`.
Unchanged Arabic CLI SHA256: `e5b05bfa6dfcf6852ee922e661074eeac2089a13d1b603d10d2060d72f547d8c`.
Unchanged Arabic service SHA256: `6005906b68791a3cc7e9f2cb3af45f4772e50670735a84cac24423bdfbd3dc87`.

# Arabic daemon independent review

Reviewer: `/root/review_web_ar`  
Date: 2026-10-18  
Result: PASS for Arabic language fidelity after corrections.

Read all 194 browser messages (including every Arabic plural branch), all 87 terminal entries including the complete help text, and all 28 platform-service messages against English. This reviewer did not author these translations.

Corrected the browser input-sensor explanation to say that input must reach the configured sensitivity threshold while monitoring is armed; the lookup key remains unchanged. Corrected three terminal formulations: grammatical agreement for restoring armed state, the number of clients receiving a test, and a count-neutral connected-client label. The service catalog required no edit.

Checked the distinction between enabling/stopping monitoring and silencing an alarm, local pairing key versus account key, PIN requirements, sleeping-phone delivery limitations, location opt-ins, updates, sensor events, Arabic plural categories, units and durations. Checked every CLI Go-format argument in order, including color arguments, literal commands, flags, config keys and paths. Checked Windows logon tasks, macOS LaunchAgent permissions and Linux linger prerequisites. Arabic macOS labels match [Apple’s Arabic Privacy & Security documentation](https://support.apple.com/ar-qa/guide/mac-help/-mchl211c911f/mac): إعدادات النظام، الخصوصية والأمن، تسهيلات الاستخدام. Commands and tool names remain literal.

Validation: exact 194/87/28 key sets, exact named-variable multisets for all plural branches, and ordered Go-format sequences passed; `python3 tools/localization/check.py` also passed. The companion manifest hashes all three English and all three Arabic catalogs.

This is an independent AI language review, not native-human certification or full platform execution. The separately tracked browser authentication incompatibility with the v2 server remains a functional integration issue; this report does not claim successful browser pairing or notification end-to-end tests. RTL terminal rendering depends on terminal support. No shared code or generated assets were changed.

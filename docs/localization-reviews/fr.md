# French daemon independent review

PASS — 2026-10-18. Reviewer: `/root/review_web_fr`, independent of the catalog author.

Read and compared every entry: 194 browser messages, 87 terminal messages including the complete multiline help, and 28 platform-service messages (309 total). Reviewed all plural branches, sensor events, arming/disarming versus dismissing an alert, local pairing keys versus account keys, phone sleep/connectivity limits, update behavior, units and configurable thresholds.

Updated the browser input-sensor explanation to the final configured-sensitivity threshold source while retaining its original JSON key. Improved the connection explanation and the description of surveillance precision. Restored the omitted “in the configuration” instruction in two terminal settings messages. Existing service translations accurately retain Windows logon scheduled tasks, the French macOS path Réglages Système → Confidentialité et sécurité → Accessibilité, and the explicit Linux linger command requirement. Commands, flags, paths, URLs and diagnostic markers remain literal; no service catalog edits were needed.

Validation: all three exact source/target key sets (194/87/28), every named-variable multiset in every plural branch, nonempty values and ordered Go formatting verbs passed. The JSON companion records exact final source/target file-byte hashes.

Limits: independent agent linguistic review, not a native-human certification or a live OS walkthrough. Existing browser protocol compatibility, integrated rendering, builds, CI and Sonar remain integration concerns; this review does not claim they passed. No unresolved French meaning issue was found in this scope.

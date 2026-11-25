# Vietnamese daemon independent review

Status: PASS — agent linguistic and source-fidelity review.
Reviewer: /root/review_web_vi

Read and compared all 194 browser, 87 terminal and 28 platform-service entries against their English sources. The reviewer was independent of the translation author. Corrected the browser input-sensor description to explain its configured sensitivity threshold, keeping the original JSON key. Restored the explicit configuration-file reference in the terminal restore_armed_state instruction.

Verified natural Vietnamese arming/disarming versus silencing wording, local pairing keys versus accounts, browser suspension and conditional connectivity, optional location and GitHub queries, six-sensor labels and alerts, durations and quantities, and command usage. Reviewed the complete help text and Windows logon task, macOS accessibility permission and Linux linger instructions. Commands, file paths, diagnostic markers and technical identifiers remain literal. Vietnamese plural objects use the applicable other branch.

Validation: all three exact key sets (194/87/28), named-variable multisets, and ordered Go formatting verbs passed. `python3 tools/localization/check.py` passed across all 25 browser catalogs. Manifest hashes cover all three source and target files.

Limitations: independent agent review is not native-human certification, OS UI testing or end-to-end pairing validation. Existing browser/server authentication compatibility is outside this linguistic review; no protocol changes or runtime success are claimed. Root owns integration, font generation, builds, CI and Sonar.


## Windows physical-motion browser delta — 2026-11-02

Independent follow-up reviewer: `/root/review_alarm_photo_languages/review_existing_vi`. This identity differs from the historical daemon reviewer; the earlier review remains intact and is not attributed to this reviewer. Read the complete three-message delta against the frozen English browser source and daemon translation contract, independently of the initial draft author.

Accepted `MOTION` as “CHUYỂN ĐỘNG” and `Device moved!` as “Thiết bị đã di chuyển!”. Corrected the new sensor explanation to explicitly say it triggers an alarm when movement is detected while protection is enabled. “Armed” describes protection, not weapons. The physical-movement/no-location-access distinction is preserved; no GPS tracking, universal Windows/Mac hardware support or every-movement guarantee is implied. These are local browser/daemon messages and do not add remote camera or cloud behavior.

Compared with snapshot `localization-baseline.json` and baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`: all **194 prior browser values are exact, with three additions = 197**. All **87 CLI and 28 service values** match the snapshot, and both complete target files are byte-identical to that baseline commit. Those unchanged catalogs retain their historical semantic review; no fresh complete review is claimed here.

Scoped browser placeholder/plural dictionary validation, CLI/service structure and ordered Go format checks, and `reviews.validate(locales={'vi'}, require_all=True)` passed. The manifest includes the actual three source hashes and all three target hashes. No code, CLI/service text, shared generated assets or fonts changed in this review. This is an AI source-fidelity delta review, not native-human certification, hardware/device testing, whole-project CI or release approval.

Final English browser SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Final Vietnamese browser SHA256: `90844f58d67a5386711a46efb15dd938da0adbf72afa7b4f28340345985e3f66`.
Unchanged Vietnamese CLI SHA256: `8cb37f27db3ffc734d177b1b6699e6638efed87849fa49477a32bb6b3d013861`.
Unchanged Vietnamese service SHA256: `4c6e1dedef4b72c86dd851619e1f74bc0efca32238ebaa8755968cae4c2372b3`.

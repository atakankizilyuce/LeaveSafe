# Indonesian daemon independent review

Reviewer: `/root/review_web_id`  
Result: PASS  
Scope: All 194 browser messages, 87 terminal messages and 28 platform-autostart instructions, individually compared against English.

Corrected the keyboard/trackpad hint to describe the configured sensitivity threshold while protection is active (retaining its original JSON key). Clarified the terminal error for resolving the actual program path. All other entries, including the complete terminal help and service catalogs, were reviewed and retained.

Checked the distinction between enabling protection, disabling protection and silencing alarms; local pairing keys versus account credentials; browser suspension and conditional delivery; Indonesian plural `other` forms and time units; location/update behavior; literal commands, paths and Go format ordering. macOS permission names remain Pengaturan Sistem → Privasi & Keamanan → Aksesibilitas. Windows instructions describe a logon scheduled task, and Linux logout/boot persistence requires the explicit shown command.

Read-only validation passed exact 194/87/28 key sets, named-placeholder multiplicities in every plural branch and exact ordered Go formatting verbs. The companion JSON records final file-byte hashes of all three source and target catalogs.

This is independent agent linguistic validation, not native-human certification or an end-to-end pairing/runtime test. The existing browser authentication compatibility concern remains an integration matter; this review does not claim it is resolved. No generated assets or shared implementation were edited.


## Windows motion browser delta — 2026-11-02

Independent reviewer: `/root/review_alarm_photo_languages/review_existing_id`. This follow-up reviewer differs from historical reviewer `/root/review_web_id`; the original report remains intact. Independently reviewed only the three new browser messages against English and the daemon translation contract, including their use for physical movement while protection is active. This is not a fresh full-catalog semantic review or native-human, runtime, device or store certification.

Accepted all three drafts without correction: `MOTION` → `GERAKAN`, `Device moved!` → `Perangkat bergerak!`, and the description `Memantau gerakan fisik tanpa akses lokasi. Memicu alarm saat gerakan terdeteksi ketika perlindungan aktif.` The text means physical movement, explicitly excludes location access, and conditions triggering on detected movement during active protection. It does not refer to weapons, GPS tracking, universal Windows hardware availability, native Mac support, or a guarantee that every movement is detected. Reuse of app-like terminology was independently assessed in the daemon's local browser context; no cloud/photo behavior was introduced.

Scoped validation passed: duplicate-key-rejecting JSON read; all 194 prior browser values unchanged and exactly three additions = 197; unchanged 87 CLI and 28 service values versus the snapshot and byte-for-byte files versus baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`; complete named-variable/plural contracts and ordered Go formats; and `reviews.validate(locales={'id'}, require_all=True)`. CLI/service validation and hashing do not claim a new semantic review of those catalogs. No shared assets, code, CLI or service files were edited.

Source hashes: `{"internal/localization/catalogs/en.json": "53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150", "internal/localization/service/en.json": "41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07", "web/src/locales/en.json": "c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e"}`.
Target hashes: `{"internal/localization/catalogs/id.json": "e560c5ecc4a1f23c2d7dccf4cb59704f54695dfb1379a31c95e5af180043caf5", "internal/localization/service/id.json": "375d4e18d089692f2961e524524ccc0337760ef3b31d19d0d0434b4cfdb39394", "web/src/locales/id.json": "a013d5fa319ff4baa1272eb3b824c9974903a203e17ac47f7736ab29c3c6fb34"}`.

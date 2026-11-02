# Traditional Cantonese daemon independent review

Reviewer: `/root/review_web_yue`  
Date: 2026-10-18  
Result: PASS — agent linguistic and source-fidelity review.

Read and compared every entry in the 194-message browser catalog, 87-message terminal catalog and 28-message platform service catalog (309 entries). This reviewer did not author the original catalogs.

Corrected the browser input sensor description to state that input must reach the configured sensitivity threshold while armed. All other browser and terminal/service entries were retained after full review. Arming, disarming and dismissing an alarm remain distinct; the local pairing key is not an account credential. Conditional phone delivery, suspended browser behavior, local monitoring, optional location lookups, positioning precision, update checks and plural/count units retain source meaning.

Checked the complete terminal help, Go format argument order (including ANSI color arguments), literal commands, config field names, filenames, diagnostic markers and line breaks. macOS instructions use the Hong Kong Traditional Chinese menu labels 系統設定 → 私隱與保安 → 輔助使用. Windows describes a scheduled logon task, and Linux lingering requires the displayed command. No service-catalog changes were necessary.

Validation: `python3 tools/localization/check.py` passed across all catalogs. Additional read-only checks verified exact 194/87/28 key sets, named-variable multisets for every plural branch, and ordered Go formatting verbs for this locale. The manifest hashes all three final source and target files.

No unresolved translation defect identified. Agent validation is not native-human certification, rendered font/layout certification or a successful end-to-end pairing test. The previously identified browser authentication compatibility issue remains outside this linguistic review and must not be inferred resolved.


## Windows motion: three-message delta review — 2026-11-02

Independent AI reviewer: `/root/review_alarm_photo_languages/review_existing_yue_hant`. This is a different actual reviewer from historical `/root/review_web_yue` above; that earlier full review is preserved, not attributed to the current reviewer. Reviewed the coordinator-authored three additions against frozen English and nearby Cantonese browser context. Reuse of application wording does not substitute for this daemon-context review.

`MOTION` → `移動` correctly labels physical movement. `Device moved!` → `裝置郁動咗！` correctly reports device movement; corrected its exclamation mark to Traditional Chinese punctuation. The explanatory message explicitly describes monitoring physical movement without location access and triggering when movement is detected while protection is enabled. `保護啟用期間` conveys armed monitoring, not weapons or an OS lock. None of these strings adds GPS access, a guarantee to detect every movement, cloud delivery, or macOS support. The three new messages have no named placeholders or plural dictionaries.

Validation against snapshot baseline `0d2dfbfe8b608557c947138bef2693b28494fc78`: all 194 prior browser values are exact, plus three additions = 197. CLI 87 and service 28 entries are unchanged in the snapshot and byte-identical to the baseline commit; their earlier semantic review remains the evidence. Duplicate-key-aware parsing and scoped `check.validate` passed for all three catalogs; terminal/service Go format checks also passed. `reviews.validate(root, locales={'yue-Hant'}, require_all=True)` passed with the actual current reviewer and all three source/target hashes. This is a focused three-message semantic review, not a fresh full-catalog, native-human, rendered UI, hardware, CI or store certification. No unresolved semantic issue remains in the three-message scope.

Current source SHA-256:

- `web/src/locales/en.json`: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`
- `internal/localization/catalogs/en.json`: `53580ade3de6397aacc4274c6fec16ef897b39d1e40c4a55a3a7511f86430150`
- `internal/localization/service/en.json`: `41969649e89ecb7cb5ea71d37cd9b0647d02fff0659e015192f1fabb3a042e07`

Current target SHA-256:

- `web/src/locales/yue-Hant.json`: `5e88df94fc6f8f4b2e692df9d29e1f043bac6b1600073bd02fc8ed7a4cd9eabe`
- `internal/localization/catalogs/yue-Hant.json`: `2a3c8c7ce088a3e9806c6dd11e583df10ad96ba5b8886374f2411fa283dcfa80`
- `internal/localization/service/yue-Hant.json`: `4506ff66f562aca50a76ee6fa1ae86005846e24cb46b8e11b2651c0b95de91f5`

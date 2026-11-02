# Turkish browser, terminal and service independent review

PASS — 2026-10-18. Reviewer: `/root/review_web_tr`.

Read all 194 browser messages, all 87 terminal messages (including the full help text) and all 28 platform service instructions against English: 309 entries. Reviewed complete plural variants, named variables, Go formatting and argument order, local pairing-key meaning, sensor events, arming/disarming versus silencing, location limits, update checks and phone-sleep delivery limitations.

Corrected the input-sensor explanation to trigger at the configured sensitivity threshold while armed. Made protection actions explicit as “Korumayı aç/kapat,” including the hold action and settings prerequisite; made state badges and settings-group wording consistent. “Available” in the version field now explicitly means the available version. Terminal and service catalogs needed no semantic changes. Confirmed the Turkish macOS menu path “Sistem Ayarları → Gizlilik ve Güvenlik → Erişilebilirlik,” the Windows user-logon scheduled task, and the Linux command required for linger/boot behavior. Commands, paths, flags and diagnostic markers remain literal.

Validation: exact 194/87/28 key sets, every named-variable multiset for every plural branch, and the ordered Go format verbs for all terminal/service messages passed. English browser SHA matches the approved frozen source; terminal and service source hashes match their contracts.

This is independent agent linguistic validation. It is not native-human certification, live operating-system testing, a cryptographic-protocol review or proof that notifications will arrive. Known integration limitations remain the integration owner's responsibility. No unresolved Turkish meaning issue found within this scope.


## Windows motion browser delta review — 2026-11-02

Independent reviewer: `/root/review_alarm_photo_languages/review_existing_tr`. This is the existing Turkish app reviewer performing the requested daemon delta review; the historical complete daemon reviewer was `/root/review_web_tr`. The historical review above is preserved and is not being attributed to the current reviewer. The coordinator authored the incoming three translations. This is an independent AI semantic review of those additions, not a new full browser/terminal/service review, native-human certification, device QA or store certification. Baseline commit: `0d2dfbfe8b608557c947138bef2693b28494fc78`, with comparison to the supplied `leavesafe-gallery-motion/localization-baseline.json`.

Read the final English source, the three Turkish additions, nearby existing power/lid/input sensor descriptions and movement events, and `tools/localization/TRANSLATING.md`. Accepted all three incoming additions unchanged:

- `MOTION` → “HAREKET”: physical movement sensor label, consistent with the meaning of the app's motion label.
- `Device moved!` → “Cihaz hareket etti!”: the device physically moved; no location or travel claim.
- The sensor explanation → “Konuma erişmeden fiziksel hareketi izler. Koruma açıkken hareket algılandığında tetiklenir.”: no location access, and triggering only when movement is detected with protection active. “Koruma açıkken” refers to enabled protection, never weapons. The statement does not claim all movement is detectable, promise GPS tracking, or add macOS hardware support. It is suitable for the local daemon sensor context and adds no mobile/cloud delivery behavior.

Validation passed: exact **194 prior browser values unchanged + 3 additions = 197**, source/target key and plural structure checks via `check.validate`, named-variable multisets for all browser branches, and unchanged 87-message CLI / 28-message service catalogs. CLI/service data equal the snapshot and file bytes exactly equal baseline Git blobs; their existing reviewed hashes remain unchanged. Scoped terminal/service placeholder and Go-format checks passed as preservation checks, without a fresh linguistic audit. `reviews.validate(root=daemon_root, locales={'tr'}, require_all=True)` passed with all three real source and target hashes. No semantic correction or unresolved blocker was found in the three additions. No CLI/service, implementation, framework, shared generated asset, or other locale was edited. Runtime, hardware, narrow-layout and accessibility testing remain integration work.

Reviewed browser English SHA256: `c68e38161ad02fa912f390b6db9e0e010e359aa8e5e0fcd2e08062c8bb2a838e`.
Reviewed `web/src/locales/tr.json` SHA256: `656d6750d3a95f918cd3aba833c5a71ebeb32582181086cd305d9f21a9b98721`.
Preserved CLI SHA256: `b078c652cec7c70cb507fe2cdbe3c129c79502f149e8ee28bd63d5bdde431fcc`.
Preserved service SHA256: `a734739c0babaee4ce3a67c5aa499f16f5b2eea923fd5ce808412d0f7823066f`.

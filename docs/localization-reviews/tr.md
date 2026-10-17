# Turkish browser, terminal and service independent review

PASS — 2026-10-18. Reviewer: `/root/review_web_tr`.

Read all 194 browser messages, all 87 terminal messages (including the full help text) and all 28 platform service instructions against English: 309 entries. Reviewed complete plural variants, named variables, Go formatting and argument order, local pairing-key meaning, sensor events, arming/disarming versus silencing, location limits, update checks and phone-sleep delivery limitations.

Corrected the input-sensor explanation to trigger at the configured sensitivity threshold while armed. Made protection actions explicit as “Korumayı aç/kapat,” including the hold action and settings prerequisite; made state badges and settings-group wording consistent. “Available” in the version field now explicitly means the available version. Terminal and service catalogs needed no semantic changes. Confirmed the Turkish macOS menu path “Sistem Ayarları → Gizlilik ve Güvenlik → Erişilebilirlik,” the Windows user-logon scheduled task, and the Linux command required for linger/boot behavior. Commands, paths, flags and diagnostic markers remain literal.

Validation: exact 194/87/28 key sets, every named-variable multiset for every plural branch, and the ordered Go format verbs for all terminal/service messages passed. English browser SHA matches the approved frozen source; terminal and service source hashes match their contracts.

This is independent agent linguistic validation. It is not native-human certification, live operating-system testing, a cryptographic-protocol review or proof that notifications will arrive. Known integration limitations remain the integration owner's responsibility. No unresolved Turkish meaning issue found within this scope.

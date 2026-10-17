# Marathi daemon independent review

- Result: PASS after corrections, 2026-10-18.
- Reviewer: `/root/review_web_mr`, independently assigned only Marathi, separate from its author.
- Scope: all 194 browser, 87 CLI and 28 service messages (309 total), compared with the complete English source.

Updated the browser input sensor explanation to the configured sensitivity threshold without changing its source key. Corrected the phone connection sentence's grammar and standardized the screen sensor label with the surrounding Marathi wording. The remaining browser messages preserve arming versus dismissing the alarm, local pairing keys, PIN checks, connection loss, optional location/update behavior, suspended browser delivery limits and sensor availability. Marathi count phrases are grammatical across the supported `other` plural branch and retain all variables.

All CLI and service messages were read and retained: commands, flags, file names, diagnostic markers, Go formatting order and split multiline paragraphs are correct. Windows describes a scheduled logon task, Linux linger remains an action the user must execute, and macOS retains the actual English System Settings → Privacy & Security → Accessibility names with a Marathi explanation. No cloud account operations were implied.

Read-only validation: global `tools/localization/check.py` passed; additionally verified exact Marathi key counts/sets, named-variable multisets in every plural branch and all Go formatting verbs in argument order.

This is independent agent linguistic validation, not native-human certification or legal-policy approval. Runtime/visual platform testing and CI are the integration owner's responsibility. This review does not resolve the separate daemon browser authentication compatibility issue. No English source, other language, shared code, generated output, store, release or Git changes were made by this reviewer. Byte hashes below bind the reviewed content.

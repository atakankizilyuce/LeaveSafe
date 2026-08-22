# SonarCloud maintainability temizliği — tasarım

Tarih: 2026-08-05
Proje: atakankizilyuce_LeaveSafe
Kaynak: SonarCloud, 32 açık MAINTAINABILITY issue, ~7h48min efor

## Karar özeti

- PR'lar `main`'e açılır, **merge kullanıcıya aittir**. Ben merge etmem.
- Davranış değiştiren iki issue kendi PR'larında, riski açıkça yazılarak.
- `cmd/leavesafe/main.go`'nun test edilmemiş dev fonksiyonları için **önce
  characterization test**, sonra extract-function. Test commit'i ayrı.
- İki TODO issue'su (`internal/bluetooth/ble.go:23`,
  `ble_unsupported.go:27`, INFO) **kapsam dışı** — açık kalacak.

## Adım 0 — PR #43'ü yeşile döndür (blokeyi kaldır)

`gh pr checks 43`: sadece SonarCloud düşüyor. Sebep issue sayısı değil,
`new_duplicated_lines_density = 5.5` (eşik 3).

Kaynak: `test/realtrigger/realtrigger_test.go` ve
`test/sandbox/linuxvm/scenarios_test.go` içine birebir kopyalanmış
`allSensors` + `onlySensor()` blokları — 21 + 21 satır / 767 yeni satır.

Çözüm: her iki dosya da zaten `test/harness`'ı import ediyor. Ortak kodu
`test/harness/sensors.go` içine `AllSensors` / `OnlySensor()` olarak taşı.

Ek: `cmd/leavesafe/dashboard_race_test.go:19` karmaşıklık 22 — gate'i bloke
etmiyor ama merge olunca 33. issue olur. Aynı PR'da düzelt.

## Adım 1..9 — 30 issue, 9 PR

| # | Branch | Kapsam | Issue |
|---|--------|--------|-------|
| 1 | `refactor/frontend-nested-ternaries` | 8× S3358 (Annunciator 88/90/92, app.tsx 281, ArmControl 106, StateHeader 25/42, Trace 53) + S7721 app.tsx:371 + S6606 app.tsx:376 | 10 |
| 2 | `refactor/phone-interface-complexity` | app.tsx:195 karmaşıklık 50→15 | 1 |
| 3 | `a11y/scrim-native-dialog` | Scrim.tsx role="dialog" → `<dialog>` (DAVRANIŞ) | 1 |
| 4 | `refactor/hub-complexity` | hub.go 1229 (53), 848 (36), 655 (23) | 3 |
| 5 | `refactor/hub-context-parameter` | hub.go:77 context field → parametre (DAVRANIŞ) | 1 |
| 6 | `refactor/internal-complexity` | config.go:258 (21), safe.go:134 (16), server.go:530 (16), monitor/input_darwin.go:35 (16), update/watcher.go:80 (25), location/parse_linux.go:81 (23), location/tracker_test.go:52 (17) | 7 |
| 7 | `refactor/publicip-and-console` | publicip.go:121 (S3776 19 + S8209 parametre gruplama), console_other.go:5 (S1186) | 3 |
| 8 | `refactor/alarm-complexity` | alarm.go:104 (26) — #43 sonrası | 1 |
| 9 | `refactor/main-startup-complexity` | main.go 1252 (113), 424 (55), 1041 (24) + characterization testler | 3 |

Toplam 30/30.

## Kısıtlar

- **`web/dist` commit'li**, `make web-verify` bayatlarsa CI düşer. PR 1, 2, 3
  hepsi `web/dist/app.js`'i değiştirir → birbirleriyle kesin çakışır. Sırayla
  merge edilmeli; önceki merge olunca sonrakini rebase et.
- PR 4 ve 5 aynı dosyaya (hub.go) dokunur → sıralı.
- PR 6, 7, 8, 9 birbirinden bağımsız.
- Commit mesajı stili: repo conventional commits kullanmıyor. Cümle şeklinde,
  emir kipinde, İngilizce açıklayıcı başlıklar ("Let the siren be tested
  without a machine that shrieks"). AI attribution YOK.

## Doğrulama

Her PR'da yerelde: `make fmt vet lint test`; frontend'e dokunanlarda ayrıca
`make web-lint web-verify`. Sonra CI yeşili beklenir, kullanıcıya bildirilir.

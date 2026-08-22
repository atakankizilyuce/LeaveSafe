# Üç platformun coverage profilini Sonar'a taşımak (Aşama 0)

Tarih: 2026-08-07
Kapsam: yalnız Aşama 0. Aşama 1-3 bu PR'ın dışında, sonunda not olarak duruyor.

## Problem

`ci.yml`'deki test matrisi üç OS'ta da coverage üretiyor:

```
run: go test ./... -v ${{ matrix.race }} -count=1 -coverprofile=coverage.out -covermode=atomic
```

Ama yükleme adımı `if: matrix.os == 'ubuntu-latest'` ile koşullu (`ci.yml:155-162`). macOS ve
Windows profilleri iş bitince runner'la birlikte siliniyor. Sonar yalnız Linux profilini
görüyor, dolayısıyla `_darwin.go` ve `_windows.go` dosyalarının tamamı %0 kapsanmış sayılıyor.

Bu, zaten yazılmış testlerin görünmemesi demek. Ölçüldü — Windows'ta yerel koşu:

| Dosya | Windows'ta gerçek | Sonar'da görünen |
|---|---|---|
| `internal/location/parse_windows.go` | 24/24 %100 | %0 |
| `internal/monitor/parse_windows.go` | 9/9 %100 | %0 |
| `internal/monitor/powershell_windows.go` | 2/2 %100 | %0 |
| `internal/monitor/lid_windows.go` | 6/29 %20.7 | %0 |

`parse_windows_test.go`, `parse_darwin_test.go`, `service_darwin_test.go`,
`volume_windows_test.go` dosyaları var ve geçiyorlar; sonuçları çöpe gidiyor.

## Beklenen kazanç

Doğrudan kazanç ölçüldüğü kadarıyla mütevazı: Windows tarafında gerçekten kapsanan ~38
satır, darwin tarafı tahminen ~35-60. Toplam **+75-100 satır ≈ +1.0-1.3 puan**
(%73.8 → ~%75).

Asıl gerekçe kazancın büyüklüğü değil: bu düzeltme yapılmadan platforma özel test yazmak
Sonar'da hiçbir şey kazandırmıyor. Aşama 1-3'ün ön koşulu.

## Tasarım

### Karar: birleştirmeyi Sonar'a bırakmıyoruz

`sonar.go.coverage.reportPaths` virgülle ayrılmış birden fazla yol kabul ediyor, ama aynı
dosya iki raporda birden geçtiğinde union mu yapıldığı yoksa üzerine mi yazıldığı
belgelenmemiş. Sonar ekibinin kendi ifadesi *"we go 100% by what the coverage reports tell
us"*. Portable dosyalar üç profilde de yer alacağı için yanlış semantik coverage'ı
**düşürebilir**.

Bu yüzden üç profili sonar job'ında kendimiz tek dosyaya indiriyoruz.
`sonar-project.properties` hiç değişmiyor — hâlâ tek bir kök `coverage.out` okuyor.

### Birleştirme

Go profil satırı: `<import-path>/file.go:122.24,163.2 1 29`
→ `$1` blok konumu, `$2` statement sayısı, `$3` çalışma sayacı.

Anahtar `$1 $2`, sayaçlar toplanır, ilk görülme sırası korunur:

```bash
{
  echo "mode: atomic"
  awk 'FNR > 1 {
         key = $1 " " $2
         count[key] += $3
         if (!(key in seen)) { seen[key]; order[++n] = key }
       }
       END { for (i = 1; i <= n; i++) print order[i], count[order[i]] }' \
    coverage-linux.out coverage-macos.out coverage-windows.out
} > coverage.out
```

Platforma özel dosyalar tek bir profilde geçtiği için blok çakışması olmuyor. Portable
dosyalar üç profilde de aynı blok sınırlarıyla geçiyor — kaynak dosya aynı olduğundan
`file:startLine.col,endLine.col` anahtarları birebir eşleşiyor. Build tag'li kod dosya
düzeyinde ayrıldığı için kısmi blok çakışması mümkün değil.

### Doğrulama (yapıldı)

İki örtüşen profil üretilip birleştirildi:

```
unique blocks: a=1363 b=101  a|b=1363  merged=1363
count mismatch: 0
covered: a=711 b=78  union=755  merged=755
OK: blok kumesi ve kapsanan kume tam olarak birlesim
```

`go tool cover -func=merged.out` çıktıyı sorunsuz parse etti. Ayrıca `part-a.out` 1974
satır ama 1363 benzersiz blok içeriyordu: Go'nun kendisi her test binary'si için tekrarlı
blok basıyor ve `go tool cover` bunları toplayarak okuyor. Script aynı davranışı üretiyor.

### `ci.yml` değişiklikleri

**test job**

- Matrise `label` alanı eklenir: `linux` / `macos` / `windows`. `matrix.os` kullanılsa
  dosya adı `coverage-ubuntu-latest.out` olurdu; `lint` job'ı zaten `goos` ile aynı deseni
  izliyor.
- `Coverage summary` ve `Coverage HTML report` adımları Linux'a özel kalır ve **yeniden
  adlandırmadan önce** çalışır, çünkü ikisi de `coverage.out` okuyor.
- Yeni adım: `mv coverage.out coverage-${{ matrix.label }}.out`.
- `Upload coverage report` adımından `if:` koşulu kalkar, artifact adı
  `coverage-report-${{ matrix.label }}` olur ve yalnız profili taşır.
- `coverage.html` kendi artifact'ına (`coverage-html`, Linux'a özel) ayrılır. Aksi hâlde
  macOS ve Windows leg'lerinde var olmayan bir yol listelenmiş olurdu.

**sonar job**

- Tek `download-artifact` adımı `pattern: coverage-report-*` ve `merge-multiple: true` ile
  üç profili de köke indirir (v8.0.1 bunu destekliyor).
- Yukarıdaki merge adımı eklenir.
- `actions/setup-go` eklenir; `go tool cover -func=coverage.out | tail -1` ile birleşik
  gerçek toplam step summary'ye basılır. Bugün özet yalnız Linux'un sayısını gösteriyor,
  yani hiçbir yerde gerçek çapraz platform rakamı görünmüyor.

**Tuzak: `coverage.html` exclusion'ı**

`sonar-project.properties` içindeki yorum, `coverage.html`'in analiz edilirse tek başına
quality gate'i düşürdüğünü yazıyor (`go tool cover -html` çıktısı bütün kaynak ağacını
gömüyor). `sonar.exclusions`'daki `coverage.html` deseni yalnız kökle eşleşiyor. Bu yüzden:

- profiller köke indirilir, alt dizine değil;
- `coverage.html` sonar job'ına hiç indirilmez.

Exclusion satırı yine de bırakılır: `make cover` çalıştırıp yerelde scanner deneyen biri
aynı tuzağa düşmesin.

**Hata davranışı**

Bir leg profil üretmezse awk eksik dosyada patlar ve job kırmızı olur. Sessizce düşük
coverage raporlamaktansa bu tercih edilir. `fail-fast: false` zaten yerinde ve sonar
`needs: [test, frontend]` ile üç leg'i de bekliyor — bu kısımlar değişmiyor.

`ci-success`'in `needs` listesi değişmiyor; sonar oraya bilerek dahil edilmemiş
(fork'larda secret yok).

## Başarı ölçütü

Birleştirme sonrası, PR'ın Sonar analizinde:

1. Genel coverage **artmalı**, ~%74.8-75.1 civarı. Düşerse merge semantiği yanlış demektir.
2. `internal/location/parse_windows.go` %0'dan ~%100'e çıkmalı.
3. `internal/monitor/parse_windows.go` %0'dan ~%100'e çıkmalı.
4. `internal/monitor/parse_darwin.go` %0'dan çıkmalı.
5. Hiçbir portable dosyanın coverage'ı **düşmemeli**.

2-4 doğrudan gözlemlenebilir olduğu için bu PR'ın gerçekten işe yarayıp yaramadığı
tahminle değil bakarak anlaşılır.

## Kapsam dışı

Aşama 1-3 ayrı PR'lar. Ölçülen boşluk, ileride referans olsun diye:

| Grup | Kapsanmayan | Coverage |
|---|---|---|
| Go, portable `internal/` | 583 | %82.1 |
| Go, yalnız-linux | 438 | %20.4 |
| Go, yalnız-darwin | 298 | %0.0 |
| Web (vitest) | 281 | %87.4 |
| Go, yalnız-windows | 249 | %0.0 |
| Go, portable `cmd/` | 246 | %77.4 |

En ucuz sonraki hedefler (hepsi %0): `cmd/leavesafe/cli.go` (56 satır, saf string
formatlama), `internal/auth/keyfile.go` (21, dosya I/O), `internal/monitor/availability.go`
(25), `internal/network/publicip.go` (50), `internal/ws/hub.go`'nun setter'ları +
`RunHeartbeat` + `dropExpiredSessions`.

Mevcut yapıyla sağlıklı tavan ~%85-88. Üstü kovalanmamalı: `ble_darwin.go` +
`clients_darwin.go` (100 satır, CoreBluetooth/CGO), `service_*.go` install/uninstall
yolları, `main()` kablolaması — ~250-350 satır. Bunları test etmek mock'u test etmek olur.

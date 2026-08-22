# Üç Platformun Coverage Profilini Sonar'a Taşıma — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** macOS ve Windows test leg'lerinin ürettiği ama şu anda atılan coverage profillerini Sonar'a ulaştırmak, böylece platforma özel dosyalar için zaten yazılmış testlerin sayılmasını sağlamak.

**Architecture:** Test matrisinin üç leg'i de profilini platform adıyla artifact olarak yükler. Sonar job'ı üçünü de indirir ve blok sayaçlarını toplayarak tek bir `coverage.out`'a birleştirir. Birleştirme Sonar'a bırakılmaz; `sonar-project.properties` hiç değişmez.

**Tech Stack:** GitHub Actions, `go test -coverprofile`, `go tool cover`, awk.

## Global Constraints

- Repo commit stili **conventional commit değil**: sentence-case, imperative, betimleyici tek satır. Örnek: `Keep the hook's count safe from a goroutine still logging`. `feat:`/`fix:` öneki kullanma.
- Commit mesajlarına AI attribution ekleme (`Co-Authored-By`, `Generated with…` vb. yok).
- Commit mesajı, branch adı, PR başlığı/gövdesi hiçbirine Claude Code session linki koyma.
- `ci.yml` içindeki tüm action'lar tam SHA ile pinlenmiş; yeni adımlarda **var olan SHA'ları birebir kopyala**, sürüm etiketiyle yazma.
- Workflow yorumları İngilizce ve mevcut ci.yml'nin ayrıntılı açıklayıcı üslubunda olmalı.
- `sonar-project.properties` bu planda **değişmiyor**.
- `docs/superpowers/` gitignore'da — bu plan ve spec commit edilmez.

---

### Task 1: Merge mantığını yerelde kanıtla

Bu görev repo'ya hiçbir şey yazmaz. Amacı, CI'a koyacağımız awk'ın gerçekten birleşim (union) ürettiğini ve `go tool cover`'ın çıktısını okuyabildiğini görmek. Merge yanlışsa portable dosyaların coverage'ı düşer ve bunu CI'da fark etmek çok geç olur.

**Files:**
- Create (scratch, repo dışı): `$SCRATCH/merge-coverage.sh`, `$SCRATCH/verify-merge.py`

**Interfaces:**
- Produces: Task 2'nin `ci.yml`'ye gömeceği awk programı, birebir aynı metin.

- [ ] **Step 1: İki örtüşen profil üret**

Repo kökünde çalıştır.

```bash
SCRATCH="C:/Users/ataka/AppData/Local/Temp/claude/C--workSpace-LeaveSafe/4b0ce090-c164-4e16-9743-e701a9c77311/scratchpad"
go test ./internal/ws/... ./internal/auth/... \
  -count=1 -coverpkg=./... -covermode=atomic -coverprofile="$SCRATCH/part-a.out"
go test ./internal/config/... ./internal/state/... \
  -count=1 -coverpkg=./... -covermode=atomic -coverprofile="$SCRATCH/part-b.out"
```

İki profil de aynı portable dosyaları farklı sayaçlarla içerir — CI'daki üç platform profilinin portable dosyalarda yaptığının aynısı.

- [ ] **Step 2: Merge scriptini yaz**

`$SCRATCH/merge-coverage.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
out=$1; shift
{
  echo "mode: atomic"
  awk 'FNR > 1 {
         key = $1 " " $2
         count[key] += $3
         if (!(key in seen)) { seen[key]; order[++n] = key }
       }
       END { for (i = 1; i <= n; i++) print order[i], count[order[i]] }' "$@"
} > "$out"
```

Profil satırı `<import-path>/file.go:122.24,163.2 1 29` biçiminde: `$1` blok konumu, `$2` statement sayısı, `$3` çalışma sayacı. Anahtar `$1 $2`, sayaçlar toplanır, ilk görülme sırası korunur.

- [ ] **Step 3: Doğrulama scriptini yaz**

`$SCRATCH/verify-merge.py`:

```python
import sys

def load(path):
    blocks = {}
    for line in open(path):
        if line.startswith('mode:'):
            continue
        loc, stmts, count = line.split()
        blocks[(loc, stmts)] = blocks.get((loc, stmts), 0) + int(count)
    return blocks

a, b, m = (load(p) for p in sys.argv[1:4])
covered = lambda d: {k for k, v in d.items() if v > 0}

assert set(m) == set(a) | set(b), 'BLOK KAYBI VEYA FAZLASI'
assert not [k for k in m if m[k] != a.get(k, 0) + b.get(k, 0)], 'SAYAC TOPLAMI YANLIS'
assert covered(m) == covered(a) | covered(b), 'KAPSANAN KUME BIRLESIM DEGIL'

print('blocks: a=%d b=%d union=%d merged=%d' % (len(a), len(b), len(set(a) | set(b)), len(m)))
print('covered: a=%d b=%d union=%d merged=%d'
      % (len(covered(a)), len(covered(b)), len(covered(a) | covered(b)), len(covered(m))))
print('OK: blok kumesi ve kapsanan kume tam olarak birlesim')
```

- [ ] **Step 4: Merge et ve doğrula**

```bash
bash "$SCRATCH/merge-coverage.sh" "$SCRATCH/merged.out" "$SCRATCH/part-a.out" "$SCRATCH/part-b.out"
python "$SCRATCH/verify-merge.py" "$SCRATCH/part-a.out" "$SCRATCH/part-b.out" "$SCRATCH/merged.out"
```

Beklenen: üç assert de geçer, son satır `OK: blok kumesi ve kapsanan kume tam olarak birlesim`.

Bir assert patlarsa Task 2'ye geçme — awk yanlış demektir ve CI'da coverage düşer.

Not: `part-a.out` satır sayısı benzersiz blok sayısından fazla çıkar. Bu normal; `go test` her test binary'si için tekrarlı blok basar ve `go tool cover` da bunları toplayarak okur. Script aynı davranışı üretiyor.

- [ ] **Step 5: `go tool cover` çıktıyı okuyabiliyor mu**

Repo kökünden (scratch dizininden değil — `go tool cover` kaynak ağacını ve `go.mod`'u bulmak zorunda):

```bash
go tool cover -func="$SCRATCH/merged.out" | tail -1
```

Beklenen: `total: (statements) NN.N%` biçiminde tek satır, hata yok.

- [ ] **Step 6: Commit yok**

Bu görev scratch'te kaldı, repo'da değişiklik yok. `git status` temiz olmalı.

---

### Task 2: `ci.yml`'de üç profili topla ve birleştir

**Files:**
- Modify: `.github/workflows/ci.yml:120-128` (test matrisi), `:141-163` (coverage adımları), `:503-506` (sonar job'ın indirmesi)

**Interfaces:**
- Consumes: Task 1'de doğrulanmış awk programı.
- Produces: sonar job'ın çalışma dizininde kök `coverage.out`. `sonar-project.properties`'teki `sonar.go.coverage.reportPaths=coverage.out` bunu okur ve değişmez.

Test job'ıyla sonar job'ı **tek commit'te** değişmeli. Artifact adı değişip sonar job'ı eski adı aramaya devam ederse branch'te CI kırık kalır.

- [ ] **Step 1: Matrise `label` alanı ekle**

`.github/workflows/ci.yml:120-128`, mevcut hâli:

```yaml
      matrix:
        include:
          - os: ubuntu-latest
            # -race needs cgo, which is only guaranteed present on the Unix runners.
            race: "-race"
          - os: macos-latest
            race: "-race"
          - os: windows-latest
            race: ""
```

Yenisi:

```yaml
      matrix:
        include:
          - os: ubuntu-latest
            # -race needs cgo, which is only guaranteed present on the Unix runners.
            race: "-race"
            label: linux
          - os: macos-latest
            race: "-race"
            label: macos
          - os: windows-latest
            race: ""
            label: windows
```

`matrix.os` yerine ayrı bir `label` var çünkü dosya adı `coverage-ubuntu-latest.out` yerine `coverage-linux.out` olsun istiyoruz; `lint` job'ı da zaten `goos` ile aynı deseni izliyor.

- [ ] **Step 2: Linux özetinin hangi platformu anlattığını söyle**

`.github/workflows/ci.yml:146`, mevcut satır:

```yaml
          echo "### Coverage: $total" >> "$GITHUB_STEP_SUMMARY"
```

Yenisi:

```yaml
          echo "### Coverage (linux leg only): $total" >> "$GITHUB_STEP_SUMMARY"
```

Bu adım Linux'a özel ve öyle kalıyor. Üç platformun birleşik gerçek sayısı Step 5'te sonar job'ında basılacak; ikisi karışmasın.

- [ ] **Step 3: Profili platform adıyla yeniden adlandır ve her leg'de yükle**

`.github/workflows/ci.yml:155-163`, mevcut hâli:

```yaml
      - name: Upload coverage report
        if: matrix.os == 'ubuntu-latest'
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: coverage-report
          path: |
            coverage.out
            coverage.html
          retention-days: 14
```

Yenisi (yeniden adlandırma, `Coverage HTML report` adımından **sonra** gelmeli — o adım hâlâ `coverage.out` okuyor):

```yaml
      # Every leg produces a profile, but only for the files its GOOS compiles.
      # Naming them apart is what lets the sonar job download all three side by
      # side; left as coverage.out they would overwrite one another.
      - name: Name the profile after the platform that produced it
        shell: bash
        run: mv coverage.out coverage-${{ matrix.label }}.out

      - name: Upload the coverage profile
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: coverage-report-${{ matrix.label }}
          path: coverage-${{ matrix.label }}.out
          retention-days: 14

      # The HTML report is for whoever is reading the run, and it is kept in its
      # own artifact so that the sonar job never downloads it. Analysing it fails
      # the quality gate on its own — see the coverage.html note in
      # sonar-project.properties.
      - name: Upload the coverage HTML report
        if: matrix.os == 'ubuntu-latest'
        uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7
        with:
          name: coverage-html
          path: coverage.html
          retention-days: 14
```

`shell: bash` şart: Windows runner'ında varsayılan kabuk PowerShell ve `mv` orada aynı şey değil.

- [ ] **Step 4: Sonar job'ının indirme adımını üç profile çevir**

`.github/workflows/ci.yml:503-506`, mevcut hâli:

```yaml
      - name: Fetch the coverage profile from the Linux test run
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          name: coverage-report
```

Yenisi:

```yaml
      - name: Set up Go
        uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7
        with:
          go-version-file: go.mod

      - name: Fetch every platform's coverage profile
        uses: actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c # v8.0.1
        with:
          pattern: coverage-report-*
          merge-multiple: true
```

`merge-multiple: true` üç artifact'ın içeriğini aynı dizine, yani repo köküne açar. Profil satırlarındaki yollar import path olduğu için dosyanın nerede durduğu önemsiz; ama kök seviyesinde tutmak `sonar.exclusions`'daki kök-eşleşmeli desenleri de bozmuyor.

`Set up Go` yeni: Step 5 `go tool cover` çalıştırıyor ve sonar job'ında bugün Go yok.

- [ ] **Step 5: Birleştirme adımını ekle**

Step 4'teki indirme adımından hemen sonra, `Fetch the phone interface's coverage` adımından önce:

```yaml
      - name: Merge the three profiles into the one Sonar reads
        shell: bash
        run: |
          # Each leg only compiled its own GOOS, so a platform file appears in
          # exactly one profile while portable files appear in all three with
          # identical block boundaries. Summing the counters per block is
          # therefore a plain union, and it is what `go tool cover` already does
          # with the duplicate blocks `go test` emits per test binary.
          #
          # Sonar accepts several report paths, but whether it unions or
          # overwrites a file present in more than one of them is undocumented,
          # and guessing wrong would silently lower every portable file. Merging
          # here keeps that decision in the repository.
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

          total=$(go tool cover -func=coverage.out | tail -1 | awk '{print $3}')
          echo "### Coverage across all three platforms: $total" >> "$GITHUB_STEP_SUMMARY"
```

Bir leg profil üretmemişse awk eksik dosyada durur ve job kırmızı olur. Sessizce düşük coverage raporlamaktansa bu tercih edilir.

- [ ] **Step 6: YAML'ı doğrula**

```bash
python -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml')); print('yaml ok')"
```

Beklenen: `yaml ok`.

- [ ] **Step 7: Eski artifact adına atıf kalmadığını doğrula**

```bash
grep -n "coverage-report\|coverage\.out\|coverage\.html" .github/workflows/ci.yml
```

Beklenen: `name: coverage-report` (tireli, sonek yok) **hiç geçmemeli**; yalnız `coverage-report-${{ matrix.label }}` ve `pattern: coverage-report-*` görünmeli.

- [ ] **Step 8: Commit**

```bash
git checkout -b ci/merge-platform-coverage
git add .github/workflows/ci.yml
git commit -m "Let the macOS and Windows coverage reach Sonar instead of being thrown away"
```

---

### Task 3: PR'ı aç ve gerçek Sonar sonucunu doğrula

Bu görevin çıktısı kod değil, kanıt. Değişikliğin işe yarayıp yaramadığı tahminle değil Sonar'a bakarak anlaşılır.

**Files:** yok.

**Interfaces:**
- Consumes: Task 2'nin commit'i.

- [ ] **Step 1: Push et ve PR aç**

```bash
git push -u origin ci/merge-platform-coverage
gh pr create --base main \
  --title "Let the macOS and Windows coverage reach Sonar instead of being thrown away" \
  --body "$(cat <<'EOF'
The test matrix runs on all three platforms and every leg writes a coverage
profile, but only the Linux leg uploaded one. macOS and Windows profiles were
discarded with the runner, so Sonar counted every `_darwin.go` and
`_windows.go` file as 0% covered — including files that already have passing
tests.

Measured on Windows locally: `internal/location/parse_windows.go` is 24/24,
`internal/monitor/parse_windows.go` is 9/9, `internal/monitor/powershell_windows.go`
is 2/2. All three read as 0% in Sonar today.

Each leg now uploads its own profile and the sonar job merges the three by
summing the per-block counters. Sonar can take several report paths, but its
merge semantics for a file present in more than one report are undocumented, so
the merge stays here rather than being guessed at.

`sonar-project.properties` is unchanged.
EOF
)"
```

- [ ] **Step 2: CI'ın yeşil olmasını bekle**

```bash
gh pr checks --watch
```

Beklenen: `test` üç leg'de de geçer, `sonar` geçer.

`sonar` job'ı "Artifact not found" ile patlarsa Task 2 Step 3 ile Step 4 uyuşmuyor demektir — artifact adı ile `pattern` birbirini tutmuyor.

- [ ] **Step 3: Birleşik toplamı step summary'de gör**

Sonar job'ının özetinde `### Coverage across all three platforms: NN.N%` satırı olmalı ve Linux leg'inin `### Coverage (linux leg only): …` satırından **yüksek** olmalı.

- [ ] **Step 4: Sonar'da beş ölçütü kontrol et**

```bash
curl -s "https://sonarcloud.io/api/measures/component?component=atakankizilyuce_LeaveSafe&metricKeys=coverage,line_coverage,uncovered_lines"
```

```bash
curl -s "https://sonarcloud.io/api/measures/component_tree?component=atakankizilyuce_LeaveSafe&metricKeys=coverage&strategy=leaves&ps=500" \
  | python -c "
import sys, json
targets = ('parse_windows.go', 'parse_darwin.go', 'powershell_windows.go', 'service_darwin.go')
for c in json.load(sys.stdin)['components']:
    if c['path'].endswith(targets):
        m = {x['metric']: x.get('value') for x in c['measures']}
        print(m.get('coverage'), c['path'])
"
```

Ölçütler:

1. Genel `coverage` **artmış** olmalı — başlangıç %73.8, beklenen ~%74.8-75.1.
2. `internal/location/parse_windows.go` %0 → ~%100.
3. `internal/monitor/parse_windows.go` %0 → ~%100.
4. `internal/monitor/parse_darwin.go` %0'dan çıkmış olmalı.
5. Hiçbir portable dosyanın coverage'ı düşmemeli.

- [ ] **Step 5: Coverage düşerse ne yapılır**

Genel coverage düştüyse veya bir portable dosya gerilediyse merge birleşim üretmiyor demektir. Merge etme; şu sırayla bak:

1. Sonar job log'unda `go tool cover -func` toplamı Linux'unkinden yüksek mi? Değilse hata awk'ta, Sonar'da değil — Task 1'i o üç gerçek profille tekrar koş (artifact'ları `gh run download` ile indir).
2. Toplam yüksek ama Sonar düşük gösteriyorsa sorun `sonar.exclusions` tarafındadır; `coverage.html`'in sonar job'ının çalışma dizinine sızmadığını doğrula (`ls coverage.html` boş dönmeli).

- [ ] **Step 6: Ölçütler karşılandıysa merge**

Beş ölçüt de sağlandığında PR merge edilebilir. Sonuç sayısını PR'a yorum olarak yaz — Aşama 1'in başlangıç noktası bu olacak.

---

## Kapsam dışı

Aşama 1-3 ayrı PR'lar. Ölçülen boşluk ve tavan tahmini spec'te:
`docs/superpowers/specs/2026-08-07-sonar-coverage-merge-design.md`

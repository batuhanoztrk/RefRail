# RefRail

[English README](../README.md) · [MIT lisansı](../LICENSE)

CLI adı `refrail`, proje yapılandırması `.refrail.json` ve kullanıcı paket dizini `refrail` olarak adlandırılır. Cihazın arayüz dili Türkçeyse Türkçe, diğer dillerde İngilizce mesajlar gösterilir. macOS kullanıcı dil listesini, Windows kullanıcı arayüzü dilini, Linux ise locale ortam değişkenlerini kullanır. İsteğe bağlı `REFRAIL_LANG=tr` veya `REFRAIL_LANG=en` ile dil açıkça seçilebilir.

Lefthook ve Go tabanlı yerel commit/branch doğrulaması. Geliştirici bilgisayarında **Node.js, Go veya Rust gerekmez**. Git ve indirilen platform paketi yeterlidir. Windows için Git for Windows'un shell ortamı gerekir.

**Lefthook'un önceden kurulu olması da gerekmez.** Dağıtım paketi uygun Lefthook binary'sini içerir; `install` onu doğrulayıcıyla birlikte kullanıcı dizinine otomatik kurar. Kontroller bu sabitlenmiş kopyayı kullanır. `npm`, Homebrew, yönetici yetkisi veya kurulum sırasında internet bağlantısı gerekmez. Bilgisayarda başka bir Lefthook sürümü varsa şirket kontrolü yine paket içindeki sürümle çalışır.

## Geliştirici: tek komutla etkinleştirme

İlk release ve paket başvuruları yayımlandıktan sonra paket yöneticisiyle kurulum:

```sh
brew install batuhanoztrk/tap/refrail
refrail install --repo "/proje/dizini"
```

```powershell
winget install --exact --id batuhanoztrk.RefRail
refrail install --repo "C:\Projeler\uygulama"
```

Nokta Bilişim'in yönettiği WinGet kaynağından kurulum:

```powershell
# Kaynağı bir kez ekleyin; zaten ekliyse yalnızca güncelleyin.
winget source add --name noktabilisim --arg https://winget.noktabilisim.com/api --type Microsoft.Rest
winget source update --name noktabilisim
winget install --exact --id batuhanoztrk.RefRail --source noktabilisim
```

Özel kaynakta `Git.Git` bağımlılığının da bulunması gerekir; güncel Portainer
stack'i bunu ekler. Sonraki sürümler için
`winget upgrade --exact --id batuhanoztrk.RefRail --source noktabilisim` kullanın.

Homebrew tap deposu `batuhanoztrk/homebrew-tap`, kısa tap adı
`batuhanoztrk/tap` şeklindedir. Homebrew macOS/Linux AMD64 ve ARM64, WinGet ise
Windows x64 ve ARM64 paketlerini kullanır. WinGet, Git for Windows bağımlılığını
da bildirir. CLI kendi paketini otomatik bulur; `--bundle` açıkça belirtilirse
o yol kullanılır. `brew upgrade` veya `winget upgrade` sonrasında mevcut
clone'lar eski sürümlerini korur; geçirmek istediğiniz her clone için
`refrail update --repo <yol>` çalıştırın. CLI'ı kaldırmadan önce hook'larını
kaldırmak istediğiniz her clone'da `refrail uninstall --repo <yol>` kullanın.

CI, tag ile taslak release üretimi ve Homebrew/Microsoft WinGet/özel WinGet dağıtım workflow'ları
hazırdır. İlk GitHub kurulumu, secret'lar ve yayın sırası için
[yayın rehberine](releasing.md) bakın. WinGet paketinin katalogda görünmesi
Microsoft'un PR doğrulaması ve kabulünden sonra gerçekleşir. Özel kaynak
Portainer API ile doğrudan güncellenir; Microsoft PR onayı bu kanal için gerekmez.

İşletim sisteminize/mimarinize uygun ZIP'i açıp içindeki `refrail` dizinine
girin. Bu paket kökünden:

```sh
./bin/refrail install --repo "/proje/dizini" --bundle "."
./bin/refrail doctor --repo "/proje/dizini"
```

Windows PowerShell:

```powershell
.\bin\refrail.exe install --repo "C:\Projeler\uygulama" --bundle "."
.\bin\refrail.exe doctor --repo "C:\Projeler\uygulama"
```

Her yeni clone için `install` çalıştırılır. Aynı paketle tekrarlamak güvenlidir. Global Git ayarı, shell profili veya uygulamanın kaynak dosyaları değiştirilmez. Kurulum Git common directory'sinde tutulur; bir clone'un bütün worktree'leri aynı sürüm ve başlangıç sınırını paylaşır. CLI `--repo` belirtilmezse mevcut repoyu kullanır.

## Günlük davranış

Politika hataları reddedilen commit başlığını, hata nedenini ve örnek Git komutunu gösterir. Desteklenen terminallerde başlık kırmızı, beklenen biçim sarı, örnek komut yeşil, kural kodu soluk görünür. Dosyaya veya pipe'a yönlendirilen çıktı düz metindir. Renkleri kapatmak için `NO_COLOR=1` kullanabilirsiniz; `TERM=dumb` da renkleri kapatır. Windows konsollarında renkler sanal terminal çıktısı etkin olduğunda kullanılır.

```text
feat(auth): biyometrik giriş ekle        → kabul
fix: kamera iznini düzelt                → kabul
feat(api)!: yanıt biçimini değiştir      → kabul
giriş düzeltildi                        → ret
fixup! feat: giriş ekle                  → ret

feat/add-login                          → kabul
fix/1234-camera-permission               → kabul
hotfix/payment-error                    → kabul
deneme                                  → ret
feat/AddLogin                           → ret
```

Commit tipleri: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`. Branch ön ekleri aynı liste ve `hotfix`. `main`, `master`, `develop` branch adından muaftır; commit mesajları denetlenir. Branch ön eki ile her commit'in tipinin aynı olması gerekmez.

Başlık en fazla 100 Unicode karakterdir. Scope ve ticket isteğe bağlıdır. Açıklama Türkçe veya İngilizce olabilir. **İlk fiziksel satır doğrudan geçerli başlık olmalıdır; başlığın önüne boş satır veya yorum eklenmez.** Body/footer öncesinde boş satır bulunmalıdır; yorumlar body bölümünde yer alabilir ve `BREAKING CHANGE:` desteklenir. Git, komut satırındaki `--cleanup` seçimini hook'a bildirmediği için ham mesaj, whitespace ve yorum temizlenmiş biçimleri birlikte doğrulanır; mesaj değiştirilmez. Yalnızca temizlendikten sonra geçerli hale gelen mesajlar reddedilir. Otomatik revert yerine `revert: ...` kullanın. Gerçek merge commit'leri muaf tutulur; normal commit'in başına `Merge` yazmak muafiyet sağlamaz.

Push kontrolü açık branch'i değil, gönderilen **hedef branch adlarını** inceler. Yeni commit'leri yeniden kontrol eder; normal commit hook'u atlanmış olsa da standart push hatalı mesajı yakalar. Kurulumdan önceki HEAD ve ataları eski geçmiş sayılır; update bu sınırı ileri taşımaz. Yeni branch'lerde hedef remote'un yerelde bilinen geçmişi de dışarıda tutulur. İlk push ve force push desteklenir. Branch silme ve tag push bu fazdaki denetimden muaftır. Shallow/eksik geçmişte otomatik fetch yapılmaz; açıklayıcı hata verilir.

## Projeye ek kısıtlar

Uygulama reposuna isteğe bağlı `.refrail.json` eklenir:

```json
{
  "commitTypes": ["feat", "fix", "docs", "chore", "revert"],
  "branchPrefixes": ["feat", "fix", "docs", "chore", "hotfix"],
  "maxHeaderLength": 72,
  "allowedScopes": ["auth", "camera", "api"],
  "requireScope": true,
  "requireTicket": true
}
```

Yalnızca ek kısıtlar konabilir. Listeler boş olamaz; ortak listedekilere yeni tip/ön ek eklenemez; 100 karakter sınırı artırılamaz. Scope listesi, kullanılmış scope'ları sınırlar; scope'u zorunlu yapmak için `requireScope` gerekir. `requireTicket` örneği: `feat/1234-add-login`. Bilinmeyen alanlar, `null` değerler ve gevşetme talepleri yapılandırma hatasıdır. JSON içinden komut çalıştırılmaz.

## Sürüm yükseltme ve geri dönüş

Yeni paketi ayrı dizine açıp içindeki CLI ile:

```sh
./bin/refrail update --repo "/proje/dizini" --bundle "."
./bin/refrail doctor --repo "/proje/dizini"
```

Geri dönüş için aynı `update` komutunu eski paketle çalıştırın. Her repo sürümünü ayrı sabitler; başka repolar kendiliğinden güncellenmez. Araç/politika dosyaları sürümlü kullanıcı dizinine kopyalanır. Wrapper, IDE PATH ayarına bağlı kalmadan seçilen paketin bootstrap binary'sini çağırır. Güncelleme ve geri dönüş, yönetilen wrapper'ları da bu pakete yönlendirir; geçmiş denetiminin başlangıç sınırı korunur. Yazma hatasında değiştirilmiş wrapper'lar eski içeriklerine geri alınır. Eski paketler otomatik silinmez: başka repolar hâlâ kullanıyor olabilir.

Varsayılan paket konumu `os.UserConfigDir()/refrail/packages` altıdır. Test veya taşınabilir kurulum için `REFRAIL_HOME` kullanılabilir; global shell ayarı gerekmez. Git metadata'sında kimlik bilgisi veya erişim token'ı saklanmaz.

```sh
./bin/refrail uninstall --repo "/proje/dizini"
```

Uninstall yalnızca yönetilen hook'ları ve repo kaydını kaldırır. Ortak paketleri ve proje JSON'unu korur. Başka hook veya `core.hooksPath` varsa ilk sürüm otomatik birleştirme yapmaz; çakışmayı raporlar. Yönetilen wrapper elle değiştirilmişse dosyayı inceleyip beklenen içeriğe geri getirin; üzerine sessizce yazılmaz.

## Diğer CLI komutları

```sh
./bin/refrail version --json
./bin/refrail commit-msg --repo "/proje" --file "/mesaj.txt"
./bin/refrail check --repo "/proje" --from BASE_SHA --to HEAD_SHA
```

`pre-push --remote origin`, Git'in stdin'den verdiği dört alanlı referans satırlarını kullanır; normal kullanımda Lefthook çağırır. `hook-run` kurulum wrapper'ının iç komutudur. Çıkış kodları: `0` başarı, `1` politika ihlali, `2` kurulum/yapılandırma/Git hatası. Git kendi üst düzey hata kodunu kullanabilir.

## Bakım: paket üretimi

Yalnızca bu repoyu geliştiren/paketleyen kişide Go 1.27+ gerekir. Çalışma kökünde:

```sh
go test ./...
go vet ./...
go run ./tools/release --version v0.1.0
```

Varsayılan hedefler: macOS/Linux/Windows, her biri amd64 ve arm64. Sadece bu bilgisayar için örnek:

```sh
go run ./tools/release --version v0.1.0 --targets darwin/arm64
```

Çıktılar `dist/v0.1.0` altına açılmış paketler, ZIP'ler ve `SHA256SUMS` olarak yazılır. Go doğrulayıcı `CGO_ENABLED=0` ile derlenir. Lefthook **2.1.17** upstream binary'leri resmi GitHub release'inden alınır; repoda sabitlenen upstream SHA-256 değerleriyle doğrulanır. İndirilenler `.cache` altında tutulur. Paket manifesti bütün dosyaların checksum'ını içerir. Runtime kontrollerinde indirme/derleme yapılmaz.

SHA-256 bütünlük denetimidir; yayımlayıcı imzası veya güvenilmeyen bir kaynağa karşı kimlik doğrulama değildir. İlk fazda paketler güvenilir bir kanaldan yerel olarak dağıtılır. Azure'a otomatik yayın veya remote oluşturulmaz.

## Tekrar oluşturulabilir yerel deneme

```sh
go run ./tools/sandbox --bundle dist/v0.1.0/darwin-arm64 --out sandbox/demo
```

Komut ayrı bir çalışma reposu, bare remote ve izole araç dizini oluşturur; gerçek şirket repolarına ve kullanıcı Git ayarlarına dokunmaz. Çıktı dizini önceden varsa üzerine yazmaz; yeni bir `--out` seçin. Ortam ve senaryo sonuçları `report.json` dosyasına yazılır. Binary-only testte PATH yalnızca gerekli Git/shell araçlarıyla sınırlandırılır; Node.js/Go/Rust kullanılmaz.

## İlk fazın sınırları

Yerel hook'lar kullanıcı tarafından devre dışı bırakılabilir. Bu çözüm sunucu tarafında atlanamaz bir zorunluluk sağlamaz. Proje JSON'u çalışma ağacından okunur; kullanıcıya karşı bir güvenlik sınırı değildir. Git hook'unu çalıştırmayan IDE/istemciler için garanti verilmez. Gerçek ekip istemcileri pilotta ayrıca denenmelidir. Diğer platformlar için binary üretmek o platformda çalıştırma testi yapıldığı anlamına gelmez.

Lisans bildirimleri: `THIRD_PARTY_NOTICES.md`.

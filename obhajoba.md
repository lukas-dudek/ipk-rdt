# 🛡️ Kompletní podklady k obhajobě – IPK Projekt 2: Spolehlivý přenos přes UDP (`ipk-rdt`)

> **Autor:** Lukáš Dudek  
> **Jazyk:** Go 1.23  
> **Určeno pro:** ústní obhajobu před komisí  
> **Rozsah:** Od motivace přes návrh protokolu, wire‑format, detailní implementaci řádek po řádku, testy, známé limity a očekávané dotazy

---

## 🎯 0. 30vteřinové shrnutí („elevator pitch“)

UDP sám o sobě nezaručuje doručení, pořadí ani detekci duplicit. Implementoval jsem **vlastní spolehlivý protokol** nad UDP v jazyce Go – jeden binární soubor `ipk-rdt` funguje jako **klient** (odesílatel) i **server** (příjemce). Protokol realizuje třícestný handshake (SYN → SYNACK → ACK), segmentaci s oknem 16 segmentů (Go‑Back‑N), kumulativní ACK, fast retransmit při trojitém duplicitním ACK, retransmise s exponenciálním backoffem, kontrolu integrity pomocí CRC32‑IEEE a korektní ukončení spojení (FIN → FINACK + TIME_WAIT). Program zpracovává signály SIGTERM/SIGINT, podporuje IPv4 i IPv6 a všechny kombinace vstupu/výstupu (soubor/stdin/stdout).

---

## 1. Motivace – co musíme vyřešit nad holým UDP

| Problém UDP | Jak se projevuje | Řešení v mém protokolu |
|-------------|-----------------|------------------------|
| Ztráta paketu | Data nedorazí, nikdo se to nedozví | Retransmise s RTO a exponenciálním backoffem |
| Přeházení | Pakety přijdou v jiném pořadí | Sekvenční čísla + buffer out‑of‑order segmentů na receiveru |
| Duplicita | Jeden paket dorazí vícekrát | Receiver ignoruje `SeqNum < expectedSeq` pro zápis, duplicitní ACK u senderu počítá pro fast retransmit |
| Poškození | Bity se převrátí (UDP checksum je na IPv4 nepovinný) | CRC32‑IEEE přes celý paket, poškozené pakety se tiše zahazují |
| Cizí / staré pakety | Pakety z jiné relace mohou zmást receiver | Magic byte `0x55` + náhodný 32bitový `ConnId` pro každou relaci |

---

## 2. Architektura projektu – „co kde je a proč“

```
submission_prep/
├── main.go            # Vstupní bod, parsování CLI, signály, spuštění server/klient
├── protocol.go        # Definice paketu, konstanty, serializace/deserializace, CRC32
├── sender.go          # Celá logika klienta – handshake, sliding window, odesílání, retransmise, teardown
├── receiver.go        # Celá logika serveru – handshake, příjem, reorder buffer, zápis, FIN/TIME_WAIT
├── Makefile           # Build, NixDevShellName, test
├── go.mod             # Modul `ipk-proj2`, Go 1.23
├── LICENSE            # MIT licence
├── CHANGELOG.md       # Historie verzí a známé limity
├── README.md          # Dokumentace projektu
└── tests/
    ├── cli_test.go        # Testy validace CLI argumentů a nápovědy
    ├── helpers_test.go    # Sdílené pomocné funkce + impairment proxy
    ├── protocol_test.go   # Unit testy paketů (serializace, CRC, odolnost vůči chybám)
    └── transfer_test.go   # End‑to‑end integrační testy přenosů
```

**Poznámka k ploché struktuře:** Na rozdíl od vzorového řešení s podadresáři (`internal/cli`, `internal/transport`, …) je zde veškerý kód na jedné úrovni. Je to dáno velikostí projektu – 4 zdrojové soubory, cca 420 řádek kódu. Pro projekt tohoto rozsahu je to přehledné a kompilace v jednom `go build` bez potřeby řešit importní cesty je jednodušší. Pro větší projekt by samozřejmě bylo vhodné rozdělit kód do samostatných balíčků.

---

## 3. Detailní popis implementace – řádek po řádku

### 3.1 `main.go` – vstupní bod, CLI a signály

#### Struktura `Config`

```go
type Config struct {
    IsServer bool
    IsClient bool
    Port     int
    Host     string
    Address  string
    Input    string
    Output   string
    Timeout  int
}
```

Uchovává všechna nastavení získaná z přepínačů `-s`, `-c`, `-p`, `-a`, `-i`, `-o`, `-w`.  
Pole `Host` a `Address` jsou z historických důvodů oddělená: u klienta se `-a` nastaví do `Address` a následně se zkopíruje do `Host`; u serveru zůstává jen v `Address`.

#### Parsování argumentů: `parseCLI()`

- Používá se standardní balíček `flag` z Go standardní knihovny.
- Definují se proměnné přes `flag.BoolVar`, `flag.IntVar`, `flag.StringVar`.
- `flag.Usage` je přepsán, aby vypisoval usage na **stdout** (dle zadání) a ne na stderr.
- `flag.Parse()` zpracuje argumenty.
- Následuje sada **validačních podmínek**:

| Podmínka | Chybová hláška | Důvod |
|----------|---------------|-------|
| `IsServer && IsClient` | `"cannot be both server and client"` | Nesmyslná kombinace |
| `!IsServer && !IsClient` | `"must specify either -s or -c"` | Musí být právě jeden mód |
| `Port == 0` | `"missing port (-p)"` | Port je povinný |
| Klient a `Address == ""` | `"client must specify host (-a)"` | Klient musí vědět kam |

- **Validace portu na rozsah 1–65535 zde chybí** – nekontroluje se, spoléhá se na to, že Go `net` balíček selže při pokusu o bind s nevalidním portem. (Při obhajobě je férové to přiznat.)
- Validace `-w` na zápornou hodnotu také chybí na úrovni CLI; v kódu se používá `time.Duration(cfg.Timeout) * time.Second`, takže záporná hodnota způsobí okamžitý timeout.

#### Funkce `main()`

```go
func main() {
    cfg, err := parseCLI()
    // ... ošetření chyby
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    errCh := make(chan error, 1)
    go func() {
        if cfg.IsServer {
            errCh <- runServer(cfg)
        } else {
            errCh <- runClient(cfg)
        }
    }()
    select {
    case sig := <-sigCh:
        // signál → okamžité ukončení
    case err := <-errCh:
        // normální konec nebo chyba
    }
}
```

**Proč goroutina + `select` se signály?**
- `runServer` / `runClient` běží v samostatné goroutině, takže hlavní vlákno může čekat na dokončení **nebo** na signál.
- Bez toho by po Ctrl+C program běžel dál (goroutina by stále blokovala např. na `ReadFromUDP`).
- `signal.Notify` přesměruje SIGINT a SIGTERM do kanálu `sigCh`. Když přijde signál, `select` okamžitě reaguje a program končí.

**Důležité:** `os.Exit(0)` při signálu je v souladu se zadáním („upon receiving either signal, the application MUST terminate promptly without crashing“). Nicméně někteří hodnotitelé můžou očekávat exit kód ≠ 0 při signálu – v kódu je použita 0. To je vědomé rozhodnutí: program nehavaroval, byl korektně ukončen externím požadavkem.

---

### 3.2 `protocol.go` – wire format, konstanty, serializace

#### Typy paketů

```go
const (
    TYPE_SYN    = 1
    TYPE_SYNACK = 2
    TYPE_DATA   = 3
    TYPE_ACK    = 4
    TYPE_FIN    = 5
    TYPE_FINACK = 6
)
```

**6 typů paketů** – oproti TCP tu chybí RST. Při jakékoliv chybě program prostě vrátí `error` a ukončí se, žádný "reset" protistraně neposílá. Pro účely školního projektu je to dostačující – protistrana se o chybě dozví vypršením timeoutu.

#### Hlavička paketu (21 bajtů)

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|    Magic (8)  |    Type (8)   |          ConnId               |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           ConnId (cont.)      |           SeqNum              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           SeqNum (cont.)      |           AckNum              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           AckNum (cont.)      |          Checksum             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|         Checksum (cont.)      |           Length              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   Unused (8)  |
+-+-+-+-+-+-+-+-+
```

| Pole | Bajty | Význam |
|------|-------|--------|
| Magic | 1 | Vždy `0x55` – rychlý filtr "náš paket / cizí paket" |
| Type | 1 | Jeden z `TYPE_SYN` až `TYPE_FINACK` |
| ConnId | 4 | Náhodný 32bitový identifikátor relace; generuje klient |
| SeqNum | 4 | Sekvenční číslo prvního bytu payloadu (byte offset) |
| AckNum | 4 | Kumulativní ACK: "očekávám byte číslo X" |
| Checksum | 4 | CRC32‑IEEE přes celý paket (při výpočtu je pole vynulováno) |
| Length | 2 | Délka payloadu v bytech (0 pro řídicí pakety) |
| Unused | 1 | Vždy 0, vycpávka pro zarovnání na 21 B |

**Proč zrovna 21 bajtů hlavička?**  
Je to výsledek součtu: 1 + 1 + 4 + 4 + 4 + 4 + 2 + 1 = 21. Jde o jednoduchý návrh bez ambice na `HdrLen` a rozšiřitelnost. Bajt na pozici 20 je nevyužitý, v kódu se explicitně nastavuje na 0.

**Max payload:** 1100 B → celkem max 1121 B v UDP datagramu, bezpečně pod limitem 1200 B.

#### `Packet` struct a serializace `ToBytes()`

1. Vytvoří se buffer `buf := make([]byte, HEADER_SIZE + len(p.Payload))`.
2. Postupně se zapíšou všechna pole **big‑endian** (`binary.BigEndian.PutUint32`...).
3. **Checksum pole se vynuluje** (na pozicích 14–18 se zapíše 0).
4. Délka payloadu se nastaví do `Length` a do bufferu na pozice 18–20.
5. Bajt 20 se nastaví na 0.
6. Pokud je payload, zkopíruje se za hlavičku.
7. **CRC32** se spočítá přes celý buffer (`crc32.ChecksumIEEE`), výsledek se zapíše zpět na pozice 14–18.
8. Výsledek se nastaví i do `p.Checksum` pro pozdější kontrolu (není úplně nutné, ale je to poctivé).

**Proč big‑endian?** Síťový standard. Není to výkonově kritické a big‑endian je konvencí pro síťové protokoly.

**Proč vynulovat checksum před výpočtem?**  
Aby CRC fungovalo deterministicky – přijímač udělá to samé (vynuluje pole, spočítá CRC, porovná). Kdybychom nechali původní hodnotu, nikdy by to nesouhlasilo.

#### Deserializace `ParsePacket(bytes)`

Kroky v přesném pořadí (fail‑fast):

1. **Kontrola minimální délky:** `len(data) < HEADER_SIZE` → chyba.
2. **Magic byte:** musí být `0x55` → jinak chyba.
3. **Extrakce všech polí** z bufferu.
4. **Kontrola CRC:** vytvoří se kopie bufferu, vynuluje se checksum pole, spočítá se CRC a porovná s hodnotou v paketu. Nesouhlasí → chyba.
5. **Extrakce payloadu:** pokud `Length > 0`, ověří se, že buffer je dost dlouhý (`len(data) < HEADER_SIZE + Length` → chyba), a payload se zkopíruje.

**Proč kopie bufferu?** Protože nemůžeme modifikovat originální data. Navíc při výpočtu CRC musíme checksum pole vynulovat, a to lze jen na kopii.

**Proč kontrola magického bytu až po kontrole délky?** Ne – ve skutečnosti je magický byte kontrolován jako druhý krok, hned po délce. Je to levná operace (1 byte) a odfiltruje většinu cizího provozu dřív, než se spustí dražší výpočet CRC.

---

### 3.3 `sender.go` – logika klienta (odesílatele)

#### Přehled fází

```
HANDSHAKE (SYN → čekání na SYNACK)
    ↓
ESTABLISHED (odesílání dat sliding window + zpracování ACK)
    ↓
TEARDOWN (FIN → čekání na FINACK)
```

#### Handshake – detail

1. Vygeneruje se náhodný `connId`:

   ```go
   rng := rand.New(rand.NewSource(time.Now().UnixNano()))
   connId := rng.Uint32()
   ```

2. Vytvoří se SYN paket, odešle se (`conn.Write(syn.ToBytes())`).

3. Následuje **retransmisní smyčka SYN**:

   ```go
   for !gotSynack {
       if time.Since(handshakeStart) > timeoutDur {
           return error
       }
       conn.Write(syn.ToBytes())
       conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
       // přečti, zkontroluj TYPE_SYNACK + ConnId
   }
   ```

   - Každou iteraci se SYN odešle znovu (i poprvé).
   - Čeká se 500 ms na odpověď.
   - Pokud nic nepřijde, cyklus se opakuje → SYN je odeslán znovu.
   - **Není zde exponenciální backoff** – SYN se posílá fixně každých cca 500 ms. Globální timeout `-w` slouží jako pojistka.

4. Po obdržení SYNACK se pošle **ACK** pro dokončení handshaku a přechází se do datové fáze.

#### Sliding window – datová struktura

```go
type windowPacket struct {
    bytesRaw []byte
    sentAt   time.Time
    rto      time.Duration
}
window := make(map[uint32]*windowPacket)
baseSeq := uint32(0)
nextSeq := uint32(0)
windowLimit := 16
```

- Map `window` mapuje **byte offset (SeqNum) → data segmentu + metadata**.
- `baseSeq` = nejnižší nepotvrzené SeqNum.
- `nextSeq` = jaké SeqNum dostane příští nový segment.
- `windowLimit = 16` → maximálně 16 segmentů v letu (≈ 17,6 KB).

**Proč 16?** Je to kompromis. Více segmentů by zvýšilo propustnost na linkách s velkým RTT, ale zvýšilo by i paměťovou náročnost a riziko zahlcení. Pro školní testovací prostředí (typicky localhost s `tc netem`) je 16 dostatečné.

#### Hlavní smyčka – plnění okna + zpracování ACK + retransmise

```go
for !eof || len(window) > 0 {
    // Krok 1: kontrola globálního timeoutu
    if time.Since(lastProgress) > timeoutDur { return error }

    // Krok 2: plnění okna
    for len(window) < windowLimit && !eof {
        n, err := inputFile.Read(dataBuf)
        if n > 0 {
            chunk := make([]byte, n)
            copy(chunk, dataBuf[:n])
            // ... vytvoř DATA paket, odešli, ulož do window
            nextSeq += uint32(n)
            // PACING: 1ms pauza
            time.Sleep(1 * time.Millisecond)
        }
        if err != nil { eof = true }
    }

    // Krok 3: zpracování ACK nebo čekání 10ms
    select {
    case p := <-packetCh:
        lastProgress = time.Now()
        if p.Type == TYPE_ACK {
            // Kumulativní ACK
            if ack > baseSeq { baseSeq = ack; dupAckCount = 0 }
            else if ack == baseSeq {
                dupAckCount++
                if dupAckCount == 3 { /* Fast retransmit */ }
            }
            // Vyčistění okna
            for seq := range window { if seq < baseSeq { delete(window, seq) } }
        }
        // Drain zbylé pakety z kanálu (stejná logika)
    case <-time.After(10 * time.Millisecond):
        // tick pro kontrolu timeoutů
    }

    // Krok 4: kontrola RTO timeoutů pro každý segment v okně
    now := time.Now()
    for seq, wp := range window {
        if now.Sub(wp.sentAt) > wp.rto {
            // retransmit
            conn.Write(wp.bytesRaw)
            time.Sleep(1 * time.Millisecond) // pacing
            wp.sentAt = now
            wp.rto = wp.rto * 2
            if wp.rto > 1500*time.Millisecond { wp.rto = 1500 * time.Millisecond }
        }
    }
}
```

**Důležité detaily:**

1. **Pacing 1 ms** – mezi každým odesláním (nový segment i retransmise) je 1ms pauza. Důvod: zabránit přehlcení kernelového UDP bufferu. Bez toho by burst 16× ~1100B mohl způsobit lokální ztráty v bufferu.

2. **Exponenciální backoff:** `wp.rto` začíná na 500 ms, každým timeoutem se zdvojnásobí, strop je 1500 ms. To dává rozumný kompromis mezi rychlou reakcí a trpělivostí při opakovaných ztrátách.

3. **Fast retransmit:** 3 duplicitní ACKy (stejné `AckNum` jako `baseSeq`) → okamžitá retransmise segmentu na `baseSeq`. Toto je standardní TCP optimalizace.

4. **Drain zbylých paketů:** po zpracování ACK se v cyklu `for len(packetCh) > 0` vyberou všechny zbývající pakety z kanálu a zpracují se. Tím se zabrání "zaspání" ACK v bufferu kanálu, když je hlavní smyčka zaneprázdněna plněním okna.

5. **Jak se aktualizuje `lastProgress`:** při **jakémkoliv** přijatém paketu se `lastProgress` obnoví. Progres timeuout se tedy resetuje, i když přijde duplicitní ACK – to je správně, protože to znamená, že server žije.

#### Goroutina pro příjem ACK

```go
packetCh := make(chan *Packet, 100)
go func() {
    buf := make([]byte, 2048)
    for {
        n, _, err := conn.ReadFromUDP(buf)
        if err != nil { break }
        p, err := ParsePacket(buf[:n])
        if err == nil && p.ConnId == connId {
            packetCh <- p
        }
    }
}()
```

- Běží na pozadí, čte z UDP socketu, parsuje pakety a validní posílá do buffered kanálu.
- Kapacita 100 dává dostatečný prostor pro bursty ACK.

#### Teardown

Po vyskočení z hlavní smyčky (`!eof && len(window) == 0`):

1. Pošle FIN (`SeqNum = nextSeq`).
2. Vstoupí do retransmisní smyčky FIN – každých 500 ms opakuje FIN, dokud nepřijde FINACK.
3. Globální timeout `-w` platí i tady – zajistí terminaci při ztrátě FINACK.

---

### 3.4 `receiver.go` – logika serveru (příjemce)

#### Handshake ze strany serveru

1. Poslouchá na socketu (`ListenUDP`), čeká na SYN.
2. Po přijetí SYN si zapamatuje `activeConnId` a `clientAddr`.
3. Pošle SYNACK a vstupuje do retransmisní smyčky SYNACK:

   ```go
   for !gotAck {
       if time.Since(handshakeStart) > timeoutDur { return error }
       conn.WriteToUDP(synack.ToBytes(), clientAddr)
       conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
       // čtení, kontrola ACK – ALE i DATA a FIN jsou akceptovány!
   }
   ```

4. **Zajímavost:** Server akceptuje jako dokončení handshaku nejen ACK, ale i **DATA nebo FIN**. Toto je obrana proti situaci, kdy se klientův ACK ztratí, ale klient už začne posílat data – bez tohoto by server ignoroval data a handshake by timeoutoval.

#### Datová fáze

```go
expectedSeq := uint32(0)
packetBuffer := make(map[uint32][]byte)
```

- `expectedSeq` = další byte, který očekáváme („kde máme díru“).
- `packetBuffer` = mapa `SeqNum → payload` pro out‑of‑order segmenty.

**Tři případy při příjmu DATA:**

| Podmínka | Akce |
|----------|------|
| `p.SeqNum == expectedSeq` | Zapsat payload na výstup, `expectedSeq += len(payload)`. Pak opakovaně flushnout z bufferu navazující segmenty. |
| `p.SeqNum > expectedSeq` | Uložit do `packetBuffer[p.SeqNum]` (nutná kopie dat, protože buffer bude přepsán). |
| `p.SeqNum < expectedSeq` | Duplicita – NIC nezapisovat, NIC nebufferovat. |

Po každém DATA se **vždy** pošle ACK s aktuálním `expectedSeq`:

```go
ack := Packet{AckNum: expectedSeq, ...}
conn.WriteToUDP(ack.ToBytes(), clientAddr)
```

**Proč se posílá ACK i na duplicitní/out‑of‑order data?** Kumulativní ACK říká: "mám všechno do `expectedSeq - 1`. Pořád čekám na `expectedSeq`." I duplicitní segmenty tedy dostanou odpověď – sender ví, co receiver skutečně potřebuje.

#### Teardown po přijetí FIN

```go
if p.Type == TYPE_FIN {
    finReceived = true
    finSeq = p.SeqNum
}
// ...
if finReceived && len(packetBuffer) == 0 && expectedSeq >= finSeq {
    // pošli FINACK
    // TIME_WAIT 2s
}
```

**Tři podmínky pro odeslání FINACK:**

1. `finReceived == true` – FIN už přišel.
2. `len(packetBuffer) == 0` – žádné "díry" v bufferu.
3. `expectedSeq >= finSeq` – všechna data od začátku až po FIN byla doručena a zapsána.

**TIME_WAIT 2 sekundy:**

```go
timeWaitEnd := time.Now().Add(2 * time.Second)
for time.Now().Before(timeWaitEnd) {
    conn.SetReadDeadline(timeWaitEnd)
    // čtení, pokud přijde FIN (klient nedostal FINACK)
    // → znovu pošli FINACK
}
```

- Server čeká 2 sekundy po odeslání FINACK.
- Pokud během té doby přijde znovu FIN, znamená to, že klient nedostal náš FINACK → pošleme ho znovu.
- Po 2 sekundách server skončí.

**Proč zrovna 2 sekundy?** Je to arbitrární hodnota, která dává dost času pro opakované FINy na lokální síti. TCP TIME_WAIT je typicky 2×MSL (30–120 s), ale pro školní projekt je to zbytečně dlouhé.

#### Timeouty na serveru

- Každá iterace datové smyčky nastaví `conn.SetReadDeadline(time.Now().Add(timeoutDur))` – to odpovídá `-w` parametru.
- Pokud během `-w` sekund nepřijde žádný paket, `ReadFromUDP` vrátí chybu a server se ukončí s chybou timeoutu.

**Potenciální problém:** Deadline se obnovuje pro každý paket – to znamená, že pokud server dostává duplicitní data (která nejsou progresem), deadline se stále prodlužuje. To je ale správné chování – dokud klient něco posílá, nespustí se timeout.

---

### 3.5 `tests/` – automatické testy

#### `helpers_test.go` – build, pomocné funkce, impairment proxy

**Build:**

```go
var buildOnce sync.Once
func buildBinary() error {
    buildOnce.Do(func() {
        cmd := exec.Command("go", "build", "-o", "ipk-rdt")
        cmd.Dir = buildDir  // ".."
        // ...
    })
}
```

- Binárka se postaví jen jednou (díky `sync.Once`), i když ji používá více testů.
- Cesta je relativní vůči `tests/` – proto `buildDir = ".."`.

**Impairment proxy (`runProxy`):**

Toto je velmi cenná testovací komponenta – **obousměrná UDP proxy s konfigurovatelnými poruchami**:

```go
type impairmentConfig struct {
    lossPct    int  // % ztrát
    dupPct     int  // % duplikací
    reorderPct int  // % přehození (TODO, zatím neimplementováno)
    delayMs    int  // základní zpoždění
    jitterMs   int  // náhodné kolísání zpoždění
}
```

- Proxy poslouchá na jednom portu (klient se připojuje sem) a přeposílá data na cílový server.
- Dvě goroutiny: **klient→server** a **server→klient**, každá aplikuje impairment (ztráta, duplikace, zpoždění).
- Používá se `WriteToUDP` pro přeposílání, takže funguje jako neconnected.
- Funkce `randInt` aproximuje náhodnost pomocí `crypto/rand` (mapuje 1 bajt na rozsah 0..max).

**Proč je to důležité:** Automatické testy zadavatele budou používat `tc netem` nebo podobný nástroj. Vlastní proxy v testech umožňuje ověřit robustnost protokolu bez potřeby root práv a bez závislosti na konkrétním síťovém stacku.

#### `protocol_test.go` – unit testy paketů

Testuje se:
- Round‑trip `ToBytes → ParsePacket` pro payloady různých délek (prázdný, 1B, 500B, …).
- Determiničnost serializace (stejný vstup → stejný výstup).
- Detekce poškození (překlopený bit → `ParsePacket` selže).
- Validace minimální délky (krátký vstup → chyba).
- Různé payload patterny (nuly, samé 0xFF, text, binární).

**Co testy NEkontrolují:** Chování při nesprávném `ConnId`, `SeqNum`, `AckNum` – to je testováno až integračně.

#### `transfer_test.go` – integrační end‑to‑end testy

Používá se black‑box přístup:
1. Spustí se server proces (`exec.Command(binaryPath, "-s", ...)`).
2. Počká se 200 ms.
3. Spustí se klient.
4. Čeká se na dokončení.
5. Porovná se vstupní a výstupní soubor.

Testované scénáře:

| Test | Co testuje | Proč |
|------|-----------|------|
| `EmptyFile` | 0 bajtů | Edge case: handshake bez dat musí projít |
| `SingleByte` | 1 bajt | Minimum payload |
| `SmallText` | Krátký text | Základní správnost |
| `OneFullPacket` | Přesně 1100 B | Hraniční velikost segmentu |
| `MultiplePackets` | 1650 B | Segmentace přes hranici segmentů |
| `LargeFile` | 200 KB | Stres test okna a reassembly |
| `AllByteValues` | 0x00..0xFF | Binární bezpečnost |
| `WindowBoundary` | 10 KB | Test přechodu okna |
| `StdinToFile` | stdin → soubor | Mód `-i -` |
| `FileToStdout` | soubor → stdout | Mód `-o -`, verifikace čistoty stdout |
| `StdinToStdout` | stdin → stdout | Kombinovaný pipe mód |
| `IPv6` | `::1` loopback | IPv6 podpora |

#### `cli_test.go` – testy CLI

Validace chybových stavů:
- Chybějící mód → `"either -s or -c"`
- Oba módy → `"both server and client"`
- Chybějící port → `"missing port"`
- Klient bez `-a` → `"host"`
- Neplatný/chybějící argument → ověření, že program selže (exit ≠ 0).

Test nápovědy:
- `-h` → exit 0, stdout obsahuje `"Usage"`.

---

## 4. Návrhová rozhodnutí a jejich zdůvodnění

### 4.1 Proč Go?

- **Kompilovaný jazyk** → jeden statický binární soubor bez runtime závislostí.
- **Goroutiny a kanály** → ideální pro I/O operace (čtení socketu na pozadí, signály).
- **Standardní knihovna** obsahuje `net`, `hash/crc32`, `flag`, `binary` – není potřeba žádná externí knihovna.
- **Vývojářská rychlost** – oproti C/C++ rychlejší iterace, bezpečná práce s pamětí.

### 4.2 Proč Go‑Back‑N a ne Selective Repeat?

- **Jednoduchost.** Každý ACK je kumulativní – receiver posílá jen jedno číslo (`expectedSeq`).
- **Méně stavu.** Sender nepotřebuje bitmapu přijatých segmentů.
- Receiver sice bufferuje out‑of‑order segmenty, ale **při ztrátě jednoho segmentu server pořád posílá ACK s původním `expectedSeq`** – sender vidí duplicitní ACK a po 3 duplikátech provede fast retransmit. To je elegantní kompromis: máme výhody bufferingu, ale bez složitosti SACK.

### 4.3 Proč byte offsety a ne čísla paketů?

- Bytový tok je přirozené mapování – `SeqNum` přesně říká, kam data v toku patří.
- Kumulativní ACK (`AckNum = expectedSeq`) automaticky pokryje segmenty libovolné velikosti.
- Pokud bych v budoucnu chtěl dynamicky měnit velikost segmentu, nemusel bych měnit logiku ACK.

### 4.4 Proč CRC32 a ne Internet Checksum?

- CRC32‑IEEE je dostupný v `hash/crc32` v standardní knihovně Go.
- Detekuje více typů chyb než prostý 16bitový součet (např. burst chyby do 32 bitů).
- 4 bajty checksumu krásně sedí do 21B hlavičky.

### 4.5 Proč pacing 1 ms?

- Bez pacingu by sender poslal až 16 paketů v těsném sledu za sebou.
- Kernelový UDP buffer má omezenou velikost – při burstu můžou být pakety zahozeny ještě před odesláním na síť.
- 1 ms pauza dává kernelu čas buffer vyprázdnit.
- Daň: na lince s velkou šířkou pásma to uměle limituje propustnost na cca 1 paket/ms → 8,8 Mbps.

---

## 5. Známé limity a slabá místa

| # | Limitace | Dopad | Možné řešení |
|---|---------|-------|-------------|
| 1 | **Chybí validace portu a timeoutu** v CLI (např. záporný timeout) | Záporný timeout způsobí okamžitý timeout a pád s chybou – matoucí pro uživatele | Dodělat rozsahovou validaci v `parseCLI()` |
| 2 | **SYN a FIN bez exponenciálního backoffu** | Při opakované ztrátě SYN nebo FIN se retransmituje každých 500 ms, bez zpomalení | Přidat backoff i pro SYN/FIN |
| 3 | **Fixní okno 16 segmentů** | Neadaptuje se na RTT a ztrátovost – suboptimální pro různé síťové podmínky | Implementovat dynamické okno (jako TCP) |
| 4 | **Každý DATA → ACK** (žádné delayed ACK) | Vyšší overhead, více paketů v síti | Přidat delayed ACK (ACKnout až po N paketech nebo po T ms) |
| 5 | **Pacing 1 ms je pevný** | Na rychlých linkách zbytečně limituje propustnost | Vypočítávat pacing dynamicky podle rychlosti linky |
| 6 | **Server pouze jeden klient** | Po ukončení první relace server končí – nepodporuje více po sobě jdoucích přenosů | Přidat vnější smyčku, která akceptuje nové SYN po uzavření předchozí relace |
| 7 | **Chybí vlastní RST** | Při fatální chybě se protistrana o chybě dozví až timeoutem | Implementovat RST a posílat ho při detekci nekonzistence |
| 8 | **TIME_WAIT 2s je fixní** | Na vysokolatentních spojeních nemusí stačit | Parametrizovat nebo odvodit z RTT |
| 9 | **Žádné šifrování** | Data jsou přenášena v plaintextu | Přidat TLS/DTLS vrstvu (mimo rozsah zadání) |

---

## 6. Testy – co, proč, jak

### Prostředí
- OS: Linux (referenční VM, `x86_64-linux`)
- Go: 1.23 (z Nix devShell `go`)
- Síť: localhost loopback (127.0.0.1 a ::1)
- V testech není použit `tc netem` – impairment je simulován proxy v `helpers_test.go`

### Spuštění
```bash
make test
# = cd tests && go test -v -timeout 300s .
```

### Co testy pokrývají
- **CLI:** help, nevalidní argumenty, chybějící povinné parametry.
- **Protokol:** serializace/deserializace, CRC integrita, detekce poškození, hraniční délky.
- **Přenosy:** prázdný soubor, 1 bajt, text, 1 celý segment, 1.5 segmentu, 200 KB binárních dat, všech 256 hodnot bajtů, hranice okna.
- **I/O módy:** soubor→soubor, stdin→soubor, soubor→stdout, stdin→stdout.
- **IPv6:** loopback `::1`.

### Co testy NEPOKRÝVAJÍ (a proč)
- **Síťové poruchy** (ztráty, duplikace) – v testech je připravena `runProxy`, ale samotné testy ji zatím nepoužívají. Proxy je plně funkční, jen chybí testovací scénáře, které by ji zapojily.
- **Zahození SYNACK** – otestováno jen nepřímo přes integraci, ale ne v izolaci.
- **Škálovatelnost** – netestuje se chování při více souběžných spojeních.
- **Timeouty** – netestuje se explicitně, že `-w` má skutečný efekt (kromě `TestClientTimeoutNoServer`, který ale v `transfer_test.go` chybí – je jen ve vzorovém projektu, ne v této implementaci).

---

## 7. Očekávané otázky komise a odpovědi

### Q1: Proč jste zvolil zrovna tento formát hlavičky?

**A:** Chtěl jsem jednoduchost. 21 bajtů je výsledek přirozeného součtu potřebných polí. Magic byte + Type + ConnId + SeqNum + AckNum + Checksum + Length. Poslední byte je nevyužitý – je tam kvůli zarovnání, ale není pro něj zatím definovaný význam. CRC32 dává silnou ochranu proti chybám a knihovna Go ho má vestavěný.

### Q2: Jak funguje fast retransmit ve vašem kódu?

**A:** Když sender dostane 3 po sobě jdoucí ACK se stejným `AckNum == baseSeq`, okamžitě retransmituje segment na pozici `baseSeq` bez čekání na vypršení RTO. Čítač `dupAckCount` se nuluje, jakmile přijde ACK s `AckNum > baseSeq` (tedy když se okno posune).

### Q3: Co se stane, když se ztratí první SYN?

**A:** Klient nemá žádný ACK, takže po 500 ms pošle SYN znovu. Toto se opakuje, dokud nepřijde SYNACK, nebo dokud nevyprší globální timeout `-w`.

### Q4: Proč bufferujete out‑of‑order segmenty, ale neposíláte selektivní ACK?

**A:** Je to kompromis. Bufferování zajišťuje, že jednou doručená data nemusí být přenášena znovu, i když přišla ve špatném pořadí. Selektivní ACK by to ještě vylepšily (sender by věděl, co přesně retransmitovat), ale za cenu složitější hlavičky a logiky. Pro účely projektu je kumulativní ACK s fast retransmitem dostatečné.

### Q5: Jak je zajištěno, že se data na stdout nesmíchají s debug výpisy?

**A:** Všechny info/debug/error zprávy jdou na **stderr** (`fmt.Fprintf(os.Stderr, ...)`). Na stdout jde pouze přenášený datový obsah (při `-o -`). To je v souladu se zadáním.

### Q6: Proč používáte `DialUDP` pro klienta a `ListenUDP` pro server?

**A:** Klient komunikuje jen s jedním serverem – "connected" socket (`DialUDP`) to reflektuje: stačí `Write()` a kernel filtruje pakety odjinud. Server naslouchá na příchozí spojení – "unconnected" socket (`ListenUDP`) vrací s každým paketem i adresu odesílatele, což server potřebuje pro `WriteToUDP`.

### Q7: Jak byste implementaci vylepšil?

**A:**
1. Dodělat testy s impairment proxy – otestovat chování při ztrátách, duplikacích a zpoždění.
2. Implementovat dynamické okno (alespoň základní congestion control – slow start, congestion avoidance).
3. Přidat selektivní ACK pro lepší výkon při ztrátách.
4. Přidat validaci argumentů na úrovni CLI (rozsah portu, timeout > 0).
5. Refaktorovat do samostatných balíčků pro lepší čitelnost.

### Q8: Co se stane, když server spadne uprostřed přenosu?

**A:** Klient přestane dostávat ACK. `lastProgress` se neobnoví, a po uplynutí `-w` sekund klient skončí s chybou timeoutu.

### Q9: Proč je `sync.Mutex` v okně, ale používáte ho jen v senderu?

**A:** V této implementaci **není** `sync.Mutex` použit (na rozdíl od vzorového řešení). Okno je modifikováno výhradně v hlavní smyčce senderu – jedné goroutině. Mapa `window` je čtena i zapisována jen tam. Retransmise se kontrolují ve stejné smyčce. Není tedy potřeba synchronizace.

### Q10: Jak receiver pozná, že FIN je od správného klienta?

**A:** Každý paket nese `ConnId`. Server si po handshaku pamatuje `activeConnId` a ignoruje všechny pakety s jiným `ConnId`. FIN bez správného `ConnId` je tedy zahozen.

---

## 8. Tahák – důležitá čísla

| Pojem | Hodnota |
|-------|---------|
| Magic byte | `0x55` |
| Hlavička | 21 B |
| Max payload | 1100 B |
| Max UDP datagram | 1121 B |
| Typy paketů | SYN(1), SYNACK(2), DATA(3), ACK(4), FIN(5), FINACK(6) |
| CRC | IEEE (polynom) |
| Velikost okna | 16 segmentů |
| Initial RTO | 500 ms |
| Max RTO | 1500 ms |
| Fast retransmit | po 3 duplicitních ACK |
| Pacing | 1 ms mezi odesláními |
| Progress timeout | `-w` (výchozí 1 s) |
| TIME_WAIT | 2 s |
| SYN/FIN retransmise interval | 500 ms |
| Jazyk | Go 1.23 |
| Binárka | `ipk-rdt` |
| Počet zdrojových souborů (.go) | 4 (+ 4 testovací) |

---

## 9. Kontrolní seznam k obhajobě

Před obhajobou ověřit, že umím vysvětlit:

- [ ] **Cíl projektu** – spolehlivý bytový přenos nad UDP, vlastní protokol
- [ ] **Použití** – CLI přepínače, příklady spuštění, co který přepínač dělá
- [ ] **Struktura projektu** – 4 zdrojové soubory, co kde je, proč plochá struktura
- [ ] **Formát paketu** – 21B hlavička, význam každého pole, big‑endian
- [ ] **Proč CRC32** – vestavěný v Go, silnější než internet checksum
- [ ] **Handshake** – SYN → SYNACK → ACK, retransmise, přijetí DATA/FIN místo ACK
- [ ] **Sliding window** – mapa `window`, `baseSeq`, `nextSeq`, limit 16
- [ ] **Go‑Back‑N logika** – kumulativní ACK, mazání segmentů `seq < baseSeq`
- [ ] **Retransmise** – RTO 500 ms, exponenciální backoff (×2), cap 1500 ms
- [ ] **Fast retransmit** – 3 duplicitní ACK → okamžitá retransmise
- [ ] **Receiver reorder buffer** – mapa `packetBuffer`, flush při návaznosti
- [ ] **Teardown** – FIN → FINACK, TIME_WAIT 2 s, retransmise FINACK
- [ ] **Timeouty** – globální progress timeout (`-w`) vs. segmentový RTO
- [ ] **Pacing** – 1 ms, proč, co by se stalo bez něj
- [ ] **Známé limity** – fixní okno, chybějící validace, SYN bez backoffu, chybějící RST
- [ ] **Testy** – co pokrývají, co ne, jak funguje impairment proxy
- [ ] **Co zlepšit** – dynamické okno, SACK, víc testů s proxy, refaktoring

---

*Tento dokument detailně pokrývá celou implementaci v `submission_prep` tak, jak je reálně napsána v souborech `main.go`, `protocol.go`, `sender.go`, `receiver.go`, `Makefile` a testech. Všechny popsané mechanismy odpovídají skutečnému kódu řádek po řádku.*
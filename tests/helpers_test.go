package tests

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

const binaryPath = "../ipk-rdt"
const buildDir = ".."

var buildOnce sync.Once
var buildErr error

// buildBinary sestaví binárku jednou
func buildBinary() error {
	buildOnce.Do(func() {
		cmd := exec.Command("go", "build", "-o", "ipk-rdt")
		cmd.Dir = buildDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildErr = fmt.Errorf("build failed: %v\n%s", err, string(out))
		}
	})
	return buildErr
}

// runServer spustí server na pozadí, vrátí cmd (pro Wait/Kill)
func runServer(t *testing.T, port int, outFile string, timeout int) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(binaryPath,
		"-s",
		"-p", fmt.Sprintf("%d", port),
		"-a", "127.0.0.1",
		"-o", outFile,
		"-w", fmt.Sprintf("%d", timeout),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	return cmd
}

// runClient spustí klienta a počká na dokončení
func runClient(t *testing.T, port int, inFile string, timeout int) error {
	t.Helper()
	cmd := exec.Command(binaryPath,
		"-c",
		"-a", "127.0.0.1",
		"-p", fmt.Sprintf("%d", port),
		"-i", inFile,
		"-w", fmt.Sprintf("%d", timeout),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

// compareFiles porovná dva soubory
func compareFiles(t *testing.T, file1, file2 string) bool {
	t.Helper()
	f1, err := os.ReadFile(file1)
	if err != nil {
		t.Fatalf("read %s: %v", file1, err)
	}
	f2, err := os.ReadFile(file2)
	if err != nil {
		t.Fatalf("read %s: %v", file2, err)
	}
	return bytes.Equal(f1, f2)
}

// createTempFile vytvoří dočasný soubor s daty
func createTempFile(t *testing.T, pattern string, data []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return f.Name()
}

func generateAllBytes() []byte {
	b := make([]byte, 256)
	for i := 0; i < 256; i++ {
		b[i] = byte(i)
	}
	return b
}

func generateRandom(size int) []byte {
	b := make([]byte, size)
	rand.Read(b)
	return b
}

func generateText(size int) []byte {
	blk := []byte("Hello IPK reliable transfer test. 0123456789 ABCDEF. The quick brown fox. ")
	res := make([]byte, size)
	for i := 0; i < size; {
		n := copy(res[i:], blk)
		i += n
	}
	return res[:size]
}

// ---------- vestavená poruchová proxy ----------

type impairmentConfig struct {
	lossPct    int
	dupPct     int
	reorderPct int
	delayMs    int
	jitterMs   int
}

// runProxy spustí obousměrnou UDP proxy se simulací chyb sítě.
//
// Architektura: dva sockety, dvě goroutiny.
//   - clientConn naslouchá na listenAddr (klient se připojuje sem)
//   - serverConn je připojený přímo na targetAddr (server)
//
// Goroutina A: čte od klienta -> aplikuje impairment -> posílá serveru (přes serverConn)
// Goroutina B: čte od serveru  -> aplikuje impairment -> posílá klientovi (přes clientConn)
func runProxy(t *testing.T, listenAddr, targetAddr string, cfg impairmentConfig) (stop func()) {
	t.Helper()

	laddr, err := net.ResolveUDPAddr("udp", listenAddr)
	if err != nil {
		t.Fatalf("proxy listen addr: %v", err)
	}
	taddr, err := net.ResolveUDPAddr("udp", targetAddr)
	if err != nil {
		t.Fatalf("proxy target addr: %v", err)
	}

	// Socket facing the client
	clientConn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		t.Fatalf("proxy client listen: %v", err)
	}

	// Socket facing the server (ephemeral local port, connected to server)
	serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		clientConn.Close()
		t.Fatalf("proxy server socket: %v", err)
	}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup

	// Track the last seen client address so server replies can be routed back
	var clientAddrMu sync.Mutex
	var activeClient *net.UDPAddr

	// mayApply returns true if we should apply a percentage-based impairment
	mayApply := func(pct int) bool {
		return pct > 0 && randInt(100) < pct
	}

	// forwardWithImpairment applies loss/dup/delay to a packet and sends it
	forwardWithImpairment := func(conn *net.UDPConn, dest *net.UDPAddr, data []byte) {
		// loss
		if mayApply(cfg.lossPct) {
			return
		}
		// duplicate: send an extra copy with the same delay
		if mayApply(cfg.dupPct) {
			go sendDelayed(conn, dest, data, cfg.delayMs, cfg.jitterMs)
		}
		sendDelayed(conn, dest, data, cfg.delayMs, cfg.jitterMs)
	}

	// Goroutine A: Client -> Server
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			clientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, cAddr, err := clientConn.ReadFromUDP(buf)
			if err != nil {
				select {
				case <-stopCh:
					return
				default:
					continue
				}
			}
			data := make([]byte, n)
			copy(data, buf[:n])

			// Remember which client we're talking to
			clientAddrMu.Lock()
			activeClient = cAddr
			clientAddrMu.Unlock()

			go forwardWithImpairment(serverConn, taddr, data)
		}
	}()

	// Goroutine B: Server -> Client
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			serverConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _, err := serverConn.ReadFromUDP(buf)
			if err != nil {
				select {
				case <-stopCh:
					return
				default:
					continue
				}
			}
			data := make([]byte, n)
			copy(data, buf[:n])

			clientAddrMu.Lock()
			dest := activeClient
			clientAddrMu.Unlock()

			if dest == nil {
				continue // client hasn't sent anything yet
			}

			go forwardWithImpairment(clientConn, dest, data)
		}
	}()

	return func() {
		close(stopCh)
		clientConn.Close()
		serverConn.Close()
		wg.Wait()
	}
}

func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	b := make([]byte, 1)
	rand.Read(b)
	return int(b[0]) * max / 256
}

func sendDelayed(conn *net.UDPConn, target *net.UDPAddr, data []byte, baseDelay, jitter int) {
	if baseDelay > 0 || jitter > 0 {
		extra := 0
		if jitter > 0 {
			extra = randInt(jitter)
		}
		time.Sleep(time.Duration(baseDelay+extra) * time.Millisecond)
	}
	conn.WriteToUDP(data, target)
}

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
		cmd := exec.Command("go", "build", "-o", binaryPath)
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

	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		t.Fatalf("proxy listen: %v", err)
	}

	stopCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer conn.Close()

		var mu sync.Mutex
		lastPacket := make(map[string]*pendingPacket)

		buf := make([]byte, 4096)
		for {
			select {
			case <-stopCh:
				return
			default:
			}
			conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, clientAddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					continue
				}
				return
			}
			data := make([]byte, n)
			copy(data, buf[:n])

			// loss
			if cfg.lossPct > 0 && randInt(100) < cfg.lossPct {
				continue
			}
			// duplicate
			if cfg.dupPct > 0 && randInt(100) < cfg.dupPct {
				go sendDelayed(conn, taddr, data, cfg.delayMs, cfg.jitterMs)
			}
			// reorder
			clientKey := clientAddr.String()
			mu.Lock()
			pending := lastPacket[clientKey]
			if cfg.reorderPct > 0 && pending != nil && randInt(100) < cfg.reorderPct {
				lastPacket[clientKey] = &pendingPacket{data: data, addr: clientAddr}
				mu.Unlock()
				go sendDelayed(conn, taddr, data, cfg.delayMs, cfg.jitterMs)
				go sendDelayed(conn, taddr, pending.data, cfg.delayMs, cfg.jitterMs)
				continue
			}
			lastPacket[clientKey] = &pendingPacket{data: data, addr: clientAddr}
			mu.Unlock()
			go sendDelayed(conn, taddr, data, cfg.delayMs, cfg.jitterMs)
		}
	}()

	return func() {
		close(stopCh)
		wg.Wait()
	}
}

type pendingPacket struct {
	data []byte
	addr *net.UDPAddr
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

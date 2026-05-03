package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNetwork_PacketLoss5(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(50 * 1024)
	in := createTempFile(t, "loss5*.in", data)
	out := filepath.Join(os.TempDir(), "loss5.out")
	defer os.Remove(in)
	defer os.Remove(out)

	proxyPort := 31001
	srvPort := 31002

	stopProxy := runProxy(t, fmt.Sprintf("127.0.0.1:%d", proxyPort), fmt.Sprintf("127.0.0.1:%d", srvPort), impairmentConfig{
		lossPct: 5,
	})
	defer stopProxy()

	srv := runServer(t, srvPort, out, 10)
	if err := runClient(t, proxyPort, in, 10); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestNetwork_PacketLoss10(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(50 * 1024)
	in := createTempFile(t, "loss10*.in", data)
	out := filepath.Join(os.TempDir(), "loss10.out")
	defer os.Remove(in)
	defer os.Remove(out)

	proxyPort := 31003
	srvPort := 31004

	stopProxy := runProxy(t, fmt.Sprintf("127.0.0.1:%d", proxyPort), fmt.Sprintf("127.0.0.1:%d", srvPort), impairmentConfig{
		lossPct: 10,
	})
	defer stopProxy()

	srv := runServer(t, srvPort, out, 15)
	if err := runClient(t, proxyPort, in, 15); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestNetwork_DelayAndJitter(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(20 * 1024)
	in := createTempFile(t, "delay*.in", data)
	out := filepath.Join(os.TempDir(), "delay.out")
	defer os.Remove(in)
	defer os.Remove(out)

	proxyPort := 31005
	srvPort := 31006

	stopProxy := runProxy(t, fmt.Sprintf("127.0.0.1:%d", proxyPort), fmt.Sprintf("127.0.0.1:%d", srvPort), impairmentConfig{
		delayMs:  50,
		jitterMs: 50,
		reorderPct: 5,
	})
	defer stopProxy()

	srv := runServer(t, srvPort, out, 15)
	if err := runClient(t, proxyPort, in, 15); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestSignal_CleanExit(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(os.TempDir(), "sigint.out")
	defer os.Remove(out)

	cmd := exec.Command(binaryPath, "-s", "-p", "31007", "-a", "127.0.0.1", "-o", out, "-w", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	
	time.Sleep(200 * time.Millisecond)
	
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	
	err := cmd.Wait()
	if err != nil {
		t.Fatalf("expected clean exit (0), got error: %v", err)
	}
}

func TestTimeout_EmptyTransfer(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(os.TempDir(), "timeout_empty.out")
	defer os.Remove(out)
	
	// Start server with 2s timeout
	start := time.Now()
	srv := runServer(t, 31008, out, 2)
	
	// Wait for server to exit due to inactivity (no client connects)
	err := srv.Wait()
	dur := time.Since(start)
	
	if err == nil {
		t.Fatal("expected server to exit with error on timeout, got success")
	}
	
	if dur < 1*time.Second || dur > 4*time.Second {
		t.Fatalf("expected timeout around 2s, got %v", dur)
	}
}

func TestTimeout_ClientExitsWhenServerDies(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	// Use a large file so it doesn't finish before the server is killed
	data := generateRandom(5000 * 1024)
	in := createTempFile(t, "clikill*.in", data)
	out := filepath.Join(os.TempDir(), "clikill.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 31009, out, 5)
	
	// kill server very quickly so the client gets stuck and times out
	go func() {
		time.Sleep(5 * time.Millisecond)
		srv.Process.Kill()
	}()

	err := runClient(t, 31009, in, 2)
	if err == nil {
		t.Fatal("expected client to fail due to timeout when server dies, but it succeeded")
	}
}

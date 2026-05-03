package tests

import (
	"io"
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
	data := generateRandom(10 * 1024)
	in := createTempFile(t, "loss5*.in", data)
	out := filepath.Join(os.TempDir(), "loss5.out")
	defer os.Remove(in)
	defer os.Remove(out)

	stop := runProxy(t, "127.0.0.1:40001", "127.0.0.1:40002", impairmentConfig{lossPct: 5})
	defer stop()

	srv := runServer(t, 40002, out, 15)
	if err := runClient(t, 40001, in, 15); err != nil {
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
	data := generateRandom(10 * 1024)
	in := createTempFile(t, "loss10*.in", data)
	out := filepath.Join(os.TempDir(), "loss10.out")
	defer os.Remove(in)
	defer os.Remove(out)

	stop := runProxy(t, "127.0.0.1:40003", "127.0.0.1:40004", impairmentConfig{lossPct: 10})
	defer stop()

	srv := runServer(t, 40004, out, 15)
	if err := runClient(t, 40003, in, 15); err != nil {
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
	data := generateRandom(10 * 1024)
	in := createTempFile(t, "jitter*.in", data)
	out := filepath.Join(os.TempDir(), "jitter.out")
	defer os.Remove(in)
	defer os.Remove(out)

	stop := runProxy(t, "127.0.0.1:40005", "127.0.0.1:40006", impairmentConfig{delayMs: 20, jitterMs: 10})
	defer stop()

	srv := runServer(t, 40006, out, 15)
	if err := runClient(t, 40005, in, 15); err != nil {
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
	out := filepath.Join(os.TempDir(), "signal.out")
	defer os.Remove(out)

	cmdSrv := exec.Command(binaryPath, "-s", "-p", "40007", "-o", out, "-w", "5")
	cmdSrv.Stdout = io.Discard
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Kill with SIGINT
	cmdSrv.Process.Signal(os.Interrupt)
	err := cmdSrv.Wait()
	if err == nil {
		t.Fatal("expected error on interrupt")
	}
}

func TestTimeout_EmptyTransfer(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(os.TempDir(), "timeout.out")
	defer os.Remove(out)

	// Start server with 1s timeout
	cmdSrv := exec.Command(binaryPath, "-s", "-p", "40008", "-o", out, "-w", "1")
	cmdSrv.Stdout = io.Discard
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}

	err := cmdSrv.Wait()
	if err == nil {
		t.Fatal("expected error (timeout), got success")
	}
}

func TestTimeout_ClientExitsWhenServerDies(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	
	// Create a large file so it takes time to transfer
	data := generateRandom(100 * 1024)
	in := createTempFile(t, "dies*.in", data)
	out := filepath.Join(os.TempDir(), "dies.out")
	defer os.Remove(in)
	defer os.Remove(out)

	cmdSrv := exec.Command(binaryPath, "-s", "-p", "40009", "-o", out, "-w", "5")
	cmdSrv.Stdout = io.Discard
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	cmdCli := exec.Command(binaryPath, "-c", "-a", "127.0.0.1", "-p", "40009", "-i", in, "-w", "2")
	cmdCli.Stdout = io.Discard
	cmdCli.Stderr = io.Discard
	if err := cmdCli.Start(); err != nil {
		t.Fatalf("client start: %v", err)
	}
	
	// Kill the server mid-transfer
	time.Sleep(100 * time.Millisecond)
	cmdSrv.Process.Kill()

	// The client should eventually time out
	err := cmdCli.Wait()
	if err == nil {
		t.Fatal("expected client to fail when server dies")
	}
}

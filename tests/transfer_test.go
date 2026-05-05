package tests

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTransfer_EmptyFile(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	in := createTempFile(t, "empty*.in", []byte{})
	out := filepath.Join(os.TempDir(), "empty.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30001, out, 5)
	if err := runClient(t, 30001, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_SingleByte(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := []byte{0x42}
	in := createTempFile(t, "byte*.in", data)
	out := filepath.Join(os.TempDir(), "byte.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30002, out, 5)
	if err := runClient(t, 30002, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_SmallText(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := []byte("Hello, IPK! This is a test message.\n")
	in := createTempFile(t, "text*.in", data)
	out := filepath.Join(os.TempDir(), "text.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30003, out, 5)
	if err := runClient(t, 30003, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_OneFullPacket(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(MAX_PAYLOAD)
	in := createTempFile(t, "1pkt*.in", data)
	out := filepath.Join(os.TempDir(), "1pkt.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30004, out, 5)
	if err := runClient(t, 30004, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_MultiplePackets(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(MAX_PAYLOAD * 3 / 2) // 1.5 packetu
	in := createTempFile(t, "multi*.in", data)
	out := filepath.Join(os.TempDir(), "multi.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30005, out, 5)
	if err := runClient(t, 30005, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_LargeFile(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(200 * 1024) // 200 KB
	in := createTempFile(t, "large*.in", data)
	out := filepath.Join(os.TempDir(), "large.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30006, out, 10)
	if err := runClient(t, 30006, in, 10); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_VeryLargeFile(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateRandom(2500 * 1024) // 2.5 MB (more than 500ms at 1ms pacing)
	in := createTempFile(t, "vlarge*.in", data)
	out := filepath.Join(os.TempDir(), "vlarge.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30015, out, 15)
	if err := runClient(t, 30015, in, 15); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_BinaryAllByteValues(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := generateAllBytes()
	in := createTempFile(t, "allbytes*.in", data)
	out := filepath.Join(os.TempDir(), "allbytes.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30007, out, 5)
	if err := runClient(t, 30007, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_WindowBoundary(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	// 10 KB je typicka velikost okna
	data := generateRandom(10 * 1024)
	in := createTempFile(t, "win*.in", data)
	out := filepath.Join(os.TempDir(), "win.out")
	defer os.Remove(in)
	defer os.Remove(out)

	srv := runServer(t, 30008, out, 10)
	if err := runClient(t, 30008, in, 10); err != nil {
		t.Fatalf("client: %v", err)
	}
	srv.Wait()
	if !compareFiles(t, in, out) {
		t.Fatal("files mismatch")
	}
}

func TestTransfer_StdinToFile(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := []byte("data from stdin\n")
	out := filepath.Join(os.TempDir(), "stdin.out")
	defer os.Remove(out)

	cmdSrv := exec.Command(binaryPath,
		"-s", "-p", "30009", "-a", "127.0.0.1",
		"-o", out, "-w", "5",
	)
	cmdSrv.Stdout = io.Discard
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	cmdCli := exec.Command(binaryPath,
		"-c", "-a", "127.0.0.1", "-p", "30009",
		"-i", "-", "-w", "5",
	)
	cmdCli.Stdin = bytes.NewReader(data)
	cmdCli.Stdout = io.Discard
	cmdCli.Stderr = io.Discard
	if err := cmdCli.Run(); err != nil {
		t.Fatalf("client: %v", err)
	}
	cmdSrv.Wait()

	got, _ := os.ReadFile(out)
	if !bytes.Equal(got, data) {
		t.Fatalf("stdin transfer mismatch:\n  want: %q\n  got:  %q", data, got)
	}
}

func TestTransfer_FileToStdout(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := []byte("file to stdout test\n")
	in := createTempFile(t, "fto*.in", data)
	defer os.Remove(in)

	var outBuf bytes.Buffer
	cmdSrv := exec.Command(binaryPath,
		"-s", "-p", "30010", "-a", "127.0.0.1",
		"-o", "-", "-w", "5",
	)
	cmdSrv.Stdout = &outBuf
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := runClient(t, 30010, in, 5); err != nil {
		t.Fatalf("client: %v", err)
	}
	cmdSrv.Wait()

	got := outBuf.Bytes()
	if !bytes.Equal(got, data) {
		t.Fatalf("file->stdout mismatch:\n  want: %q\n  got:  %q", data, got)
	}
}

func TestTransfer_StdinToStdout(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := []byte("stdin to stdout pipe\n")

	var outBuf bytes.Buffer
	cmdSrv := exec.Command(binaryPath,
		"-s", "-p", "30011", "-a", "127.0.0.1",
		"-o", "-", "-w", "5",
	)
	cmdSrv.Stdout = &outBuf
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	cmdCli := exec.Command(binaryPath,
		"-c", "-a", "127.0.0.1", "-p", "30011",
		"-i", "-", "-w", "5",
	)
	cmdCli.Stdin = bytes.NewReader(data)
	cmdCli.Stdout = io.Discard
	cmdCli.Stderr = io.Discard
	if err := cmdCli.Run(); err != nil {
		t.Fatalf("client: %v", err)
	}
	cmdSrv.Wait()

	got := outBuf.Bytes()
	if !bytes.Equal(got, data) {
		t.Fatalf("stdin->stdout mismatch:\n  want: %q\n  got:  %q", data, got)
	}
}

func TestTransfer_IPv6(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	data := []byte("IPv6 test payload\n")
	in := createTempFile(t, "ipv6*.in", data)
	out := filepath.Join(os.TempDir(), "ipv6.out")
	defer os.Remove(in)
	defer os.Remove(out)

	// server na IPv6 loopback
	cmdSrv := exec.Command(binaryPath,
		"-s", "-p", "30012", "-a", "::1",
		"-o", out, "-w", "5",
	)
	cmdSrv.Stdout = io.Discard
	cmdSrv.Stderr = io.Discard
	if err := cmdSrv.Start(); err != nil {
		t.Skipf("IPv6 probably not supported: %v", err)
		return
	}
	time.Sleep(200 * time.Millisecond)

	cmdCli := exec.Command(binaryPath,
		"-c", "-a", "::1", "-p", "30012",
		"-i", in, "-w", "5",
	)
	cmdCli.Stdout = io.Discard
	cmdCli.Stderr = io.Discard
	if err := cmdCli.Run(); err != nil {
		// IPv6 maybe not available – treat as skip
		t.Skipf("IPv6 client failed (maybe not available): %v", err)
		cmdSrv.Process.Kill()
		return
	}
	cmdSrv.Wait()

	if !compareFiles(t, in, out) {
		t.Fatal("IPv6 files mismatch")
	}
}

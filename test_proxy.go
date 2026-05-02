// +build ignore

package main

// UDP proxy pro simulaci síťových chyb – loss, reorder, duplicate, delay, jitter.
// Použití: go run test_proxy.go -listen :8000 -target :9000 -loss 20 -duplicate 10 -reorder 30 -delay 100 -jitter 50
// Všechny parametry jsou volitelné, hodnoty v procentech (0-100) nebo milisekundách.

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"sync"
	"time"
)

var (
	listenAddr  = flag.String("listen", ":8000", "address to listen on")
	targetAddr  = flag.String("target", ":9000", "address to forward to")
	lossPct     = flag.Int("loss", 0, "packet loss percentage (0-100)")
	dupPct      = flag.Int("duplicate", 0, "packet duplication percentage (0-100)")
	reorderPct  = flag.Int("reorder", 0, "packet reordering percentage (0-100)")
	delayMs     = flag.Int("delay", 0, "base delay in milliseconds")
	jitterMs    = flag.Int("jitter", 0, "additional random jitter in milliseconds")
)

type pendingPacket struct {
	data []byte
	addr *net.UDPAddr
}

// crypt/rand int in [0, max)
func randInt(max int) int {
	if max <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

func main() {
	flag.Parse()

	targetUDPAddr, err := net.ResolveUDPAddr("udp", *targetAddr)
	if err != nil {
		log.Fatalf("bad target address: %v", err)
	}

	listenUDPAddr, err := net.ResolveUDPAddr("udp", *listenAddr)
	if err != nil {
		log.Fatalf("bad listen address: %v", err)
	}

	conn, err := net.ListenUDP("udp", listenUDPAddr)
	if err != nil {
		log.Fatalf("cannot listen: %v", err)
	}
	defer conn.Close()

	fmt.Printf("UDP Proxy: %s → %s | loss=%d%% dup=%d%% reorder=%d%% delay=%dms jitter=%dms\n",
		*listenAddr, *targetAddr, *lossPct, *dupPct, *reorderPct, *delayMs, *jitterMs)

	// map client addr -> last packet for reordering
	var mu sync.Mutex
	lastPacket := make(map[string]*pendingPacket)

	buf := make([]byte, 4096)
	for {
		n, clientAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			log.Printf("read error: %v", err)
			continue
		}

		data := make([]byte, n)
		copy(data, buf[:n])

		// loss
		if *lossPct > 0 && randInt(100) < *lossPct {
			// drop silently
			continue
		}

		// duplicate – send original + extra copy
		if *dupPct > 0 && randInt(100) < *dupPct {
			go sendDelayed(conn, targetUDPAddr, data, *delayMs, *jitterMs)
		}

		// reorder – hold one packet and send the next instead
		clientKey := clientAddr.String()
		mu.Lock()
		pending := lastPacket[clientKey]
		if *reorderPct > 0 && pending != nil && randInt(100) < *reorderPct {
			// swap: send the new one first, then the old one
			lastPacket[clientKey] = &pendingPacket{data: data, addr: clientAddr}
			mu.Unlock()
			go sendDelayed(conn, targetUDPAddr, data, *delayMs, *jitterMs)
			go sendDelayed(conn, targetUDPAddr, pending.data, *delayMs, *jitterMs)
			continue
		}
		lastPacket[clientKey] = &pendingPacket{data: data, addr: clientAddr}
		mu.Unlock()

		go sendDelayed(conn, targetUDPAddr, data, *delayMs, *jitterMs)
	}
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
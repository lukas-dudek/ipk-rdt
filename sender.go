package main

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"time"
)

func runClient(cfg *Config) error {
	logf("Starting client to send to %s:%d (timeout: %ds)\n", cfg.Host, cfg.Port, cfg.Timeout)
	if cfg.Input != "" && cfg.Input != "-" {
		logf("read from file %s\n", cfg.Input)
	} else {
		logf("read from stdin\n")
	}

	// resolve address
	addrStr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	serverAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return fmt.Errorf("bad address %v", err)
	}

	// create socket
	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return fmt.Errorf("cannot dial %v", err)
	}
	defer conn.Close()

	// random connID
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	connId := rng.Uint32()

	// send SYN
	syn := Packet{
		Magic:  MAGIC_BYTE,
		Type:   TYPE_SYN,
		ConnId: connId,
		SeqNum: 0,
		AckNum: 0,
	}

	logf("Sending SYN\n")

	// retransmision loop for SYN
	handshakeStart := time.Now()
	timeoutDur := time.Duration(cfg.Timeout) * time.Second
	gotSynack := false
	buffer := make([]byte, 2048)

	for !gotSynack {
		if time.Since(handshakeStart) > timeoutDur {
			return fmt.Errorf("timeout waiting for SYNACK")
		}

		_, err = conn.Write(syn.ToBytes())
		if err != nil {
			return fmt.Errorf("failed to send SYN %v", err)
		}

		// wait for SYNACK with timeout
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// loop again and retransmit
			continue
		}

		resp, err := ParsePacket(buffer[:n])
		if err == nil && resp.Type == TYPE_SYNACK && resp.ConnId == connId {
			logf("Got SYNACK\n")
			gotSynack = true
		}
	}

	// send ACK
	ack := Packet{
		Magic:  MAGIC_BYTE,
		Type:   TYPE_ACK,
		ConnId: connId,
		SeqNum: 0,
		AckNum: 0,
	}
	conn.Write(ack.ToBytes())
	logf("Sent ACK connection established\n")

	// prepare input file or stdin
	var inputFile *os.File
	if cfg.Input == "" || cfg.Input == "-" {
		inputFile = os.Stdin
	} else {
		f, err := os.Open(cfg.Input)
		if err != nil {
			return fmt.Errorf("cannot open input file %v", err)
		}
		defer f.Close()
		inputFile = f
	}

	logf("start sending data with sliding window\n")
	dataBuf := make([]byte, MAX_PAYLOAD)

	// receive packets in background
	packetCh := make(chan *Packet, 100)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				break
			}
			p, err := ParsePacket(buf[:n])
			if err == nil && p.ConnId == connId {
				packetCh <- p
			}
		}
	}()

	// window packet struct
	type windowPacket struct {
		bytesRaw []byte
		sentAt   time.Time
		rto      time.Duration
	}

	window := make(map[uint32]*windowPacket)
	baseSeq := uint32(0)
	nextSeq := uint32(0)
	windowLimit := 16 // max packets in flight
	eof := false

	timeoutDur = time.Duration(cfg.Timeout) * time.Second
	lastProgress := time.Now()
	dupAckCount := 0

	// main loop
	for !eof || len(window) > 0 {

		// check global inactivity timeout
		if time.Since(lastProgress) > timeoutDur {
			return fmt.Errorf("timeout: no progress for %d seconds", cfg.Timeout)
		}

		// fill the window if we have space
		for len(window) < windowLimit && !eof {
			n, err := inputFile.Read(dataBuf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, dataBuf[:n])

				dataPkt := Packet{
					Magic:   MAGIC_BYTE,
					Type:    TYPE_DATA,
					ConnId:  connId,
					SeqNum:  nextSeq,
					AckNum:  0,
					Payload: chunk,
				}

				rawBytes := dataPkt.ToBytes()
				conn.Write(rawBytes)
				time.Sleep(1 * time.Millisecond) // pacing to prevent buffer overflow

				window[nextSeq] = &windowPacket{
					bytesRaw: rawBytes,
					sentAt:   time.Now(),
					rto:      500 * time.Millisecond,
				}

				nextSeq += uint32(n)
			}

			if err != nil {
				eof = true
			}
		}

		// process acks or wait a little
		select {
		case p := <-packetCh:
			lastProgress = time.Now() // server is alive
			if p.Type == TYPE_ACK {
				ack := p.AckNum
				if ack > baseSeq {
					baseSeq = ack
					dupAckCount = 0
				} else if ack == baseSeq {
					dupAckCount++
					if dupAckCount == 3 {
						// fast retransmit
						if wp, ok := window[baseSeq]; ok {
							conn.Write(wp.bytesRaw)
							time.Sleep(1 * time.Millisecond) // pacing
						}
					}
				}
				for seq := range window {
					if seq < baseSeq {
						delete(window, seq)
					}
				}
			}
			// delete packets remaining in channel
			for len(packetCh) > 0 {
				p := <-packetCh
				if p.Type == TYPE_ACK {
					ack := p.AckNum
					if ack > baseSeq {
						baseSeq = ack
						dupAckCount = 0
					} else if ack == baseSeq {
						dupAckCount++
						if dupAckCount == 3 {
							if wp, ok := window[baseSeq]; ok {
								conn.Write(wp.bytesRaw)
								time.Sleep(1 * time.Millisecond) // pacing
							}
						}
					}
					for seq := range window {
						if seq < baseSeq {
							delete(window, seq)
						}
					}
				}
			}
		case <-time.After(10 * time.Millisecond):
		}

		// check timeouts
		now := time.Now()
		for seq, wp := range window {
			if now.Sub(wp.sentAt) > wp.rto {
				logf("Timeout seq=%d, retransmitting\n", seq)
				conn.Write(wp.bytesRaw)
				time.Sleep(1 * time.Millisecond)
				wp.sentAt = now
				wp.rto = wp.rto * 2
				if wp.rto > 1500*time.Millisecond {
					wp.rto = 1500 * time.Millisecond
				}
			}
		}
	}

	logf("all data sent and acked\n")

	// teardown send FIN
	fin := Packet{
		Magic:  MAGIC_BYTE,
		Type:   TYPE_FIN,
		ConnId: connId,
		SeqNum: nextSeq,
		AckNum: 0,
	}

	logf("Sending FIN\n")

	teardownStart := time.Now()
	gotFinack := false

	for !gotFinack {
		if time.Since(teardownStart) > timeoutDur {
			return fmt.Errorf("timeout waiting for FINACK")
		}

		conn.Write(fin.ToBytes())

		select {
		case p := <-packetCh:
			if p.Type == TYPE_FINACK {
				logf("Got FINACK, closing connection\n")
				gotFinack = true
			}
		case <-time.After(500 * time.Millisecond):
			// loop and retransmit FIN
		}
	}

	return nil
}

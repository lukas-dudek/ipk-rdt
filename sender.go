package main

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"time"
)

func runClient(cfg *Config) error {
	fmt.Printf("Starting client to send to %s:%d (timeout: %ds)\n", cfg.Host, cfg.Port, cfg.Timeout)
	if cfg.Input != "" && cfg.Input != "-" {
		fmt.Printf("Will read from file: %s\n", cfg.Input)
	} else {
		fmt.Printf("Will read from stdin\n")
	}

	// resolve address
	addrStr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	serverAddr, err := net.ResolveUDPAddr("udp", addrStr)
	if err != nil {
		return fmt.Errorf("bad address: %v", err)
	}

	// create socket
	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		return fmt.Errorf("cannot dial: %v", err)
	}
	defer conn.Close()

	// get random connID for this session
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
	
	fmt.Println("Sending SYN...")
	
	// Retransmission loop for SYN
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
			return fmt.Errorf("failed to send SYN: %v", err)
		}

		// wait for SYNACK with short timeout
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// timeout, loop again and retransmit
			continue
		}

		resp, err := ParsePacket(buffer[:n])
		if err == nil && resp.Type == TYPE_SYNACK && resp.ConnId == connId {
			fmt.Println("Got SYNACK")
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
	fmt.Println("Sent ACK, connection established")

	// prepare input file or stdin
	var inputFile *os.File
	if cfg.Input == "" || cfg.Input == "-" {
		inputFile = os.Stdin
	} else {
		f, err := os.Open(cfg.Input)
		if err != nil {
			return fmt.Errorf("cannot open input file: %v", err)
		}
		defer f.Close()
		inputFile = f
	}

	fmt.Println("Start sending data with sliding window...")
	dataBuf := make([]byte, MAX_PAYLOAD)

	// receive ACKs in background
	ackCh := make(chan uint32, 100)
	go func() {
		ackBuf := make([]byte, 2048)
		for {
			n, _, err := conn.ReadFromUDP(ackBuf)
			if err != nil {
				break
			}
			p, err := ParsePacket(ackBuf[:n])
			if err == nil && p.Type == TYPE_ACK && p.ConnId == connId {
				ackCh <- p.AckNum
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
	windowLimit := 32 // max packets in flight
	eof := false

	timeoutDur = time.Duration(cfg.Timeout) * time.Second
	lastProgress := time.Now()

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
				// fmt.Printf("Sent DATA seq=%d\n", nextSeq)
				
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
		case ack := <-ackCh:
			// update base if ack is bigger
			if ack > baseSeq {
				lastProgress = time.Now() // some real progress
				baseSeq = ack
			}
			// remove acked packets from window
			for seq := range window {
				if seq < baseSeq {
					delete(window, seq)
					// fmt.Printf("Acked seq=%d\n", seq)
				}
			}
		case <-time.After(10 * time.Millisecond):
			// tick
		}

		// check timeouts
		now := time.Now()
		for seq, wp := range window {
			if now.Sub(wp.sentAt) > wp.rto {
				fmt.Printf("Timeout seq=%d, retransmitting\n", seq)
				conn.Write(wp.bytesRaw)
				wp.sentAt = now // restart timer
				wp.rto = wp.rto * 2 // backoff
				if wp.rto > 4 * time.Second {
					wp.rto = 4 * time.Second
				}
			}
		}
	}

	fmt.Println("All data sent and acked")

	// Teardown send FIN
	fin := Packet{
		Magic:  MAGIC_BYTE,
		Type:   TYPE_FIN,
		ConnId: connId,
		SeqNum: nextSeq,
		AckNum: 0,
	}

	fmt.Println("Sending FIN...")
	
	teardownStart := time.Now()
	gotFinack := false
	
	for !gotFinack {
		if time.Since(teardownStart) > timeoutDur {
			return fmt.Errorf("timeout waiting for FINACK")
		}

		conn.Write(fin.ToBytes())
		
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buffer := make([]byte, 2048)
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			// timeout, loop and retransmit FIN
			continue
		}
		
		p, err := ParsePacket(buffer[:n])
		if err == nil && p.Type == TYPE_FINACK && p.ConnId == connId {
			fmt.Println("Got FINACK, closing connection")
			gotFinack = true
		}
	}

	return nil
}

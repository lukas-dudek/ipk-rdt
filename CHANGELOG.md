# Changelog

## [1.0.0] – 2025-03-10

### Implemented Functionality

- **Three-way handshake** (SYN → SYNACK → ACK) for session establishment with retransmission and timeout.
- **Reliable data transfer** using sliding window (Go-Back-N) with cumulative acknowledgements.
- **Out-of-order buffering** at the receiver: segments arriving before their predecessor are stored and flushed in order.
- **Retransmission with exponential backoff:** initial RTO 500 ms, doubled on each timeout, capped at 1500 ms.
- **Fast retransmit:** three duplicate ACKs trigger immediate retransmission of the oldest unacknowledged segment.
- **CRC32-IEEE integrity protection** over header and payload; corrupted packets are silently dropped.
- **Connection identification** via random 32-bit `ConnId` and magic byte `0x55`.
- **Graceful session teardown** (FIN → FINACK) with 2-second TIME_WAIT on the server for FIN retransmission handling.
- **Full CLI:** supports `-s`/`-c`, `-p`, `-a`, `-i`, `-o`, `-w`, `-h`/`--help` exactly as specified.
- **IPv4 and IPv6** addressing support.
- **stdin / stdout and file I/O** for both client and server.
- **SIGTERM / SIGINT** handling for clean shutdown without temporary files.
- **Packet pacing** (1 ms inter-packet delay) to prevent local UDP buffer overflow.
- **Automated test suite** covering CLI validation, packet serialization, integrity checks, empty/small/large/binary/all-byte transfers, stdin/stdout modes, IPv6, and window-boundary scenarios.

### Known Limitations

1. **Only IPv6 loopback (`::1`) tested.** Full dual-stack testing across different network interfaces was not performed.
2. **No congestion control.** The fixed window of 16 segments does not adapt to network conditions, which may cause excessive retransmissions on congested links.
3. **No Selective Acknowledgements (SACK).** Only cumulative ACKs are used, which can lead to unnecessary retransmissions of already-buffered segments after a single loss.
4. **Single transfer per server process.** Concurrent client handling is not supported.
5. **No encryption or authentication.** Data is transmitted in plaintext; `ConnId` provides only accidental-confusion protection.
6. **TIME_WAIT is fixed at 2 seconds** and is not configurable via CLI.
7. **1 ms pacing delay** may limit throughput on high-bandwidth, low-latency links.
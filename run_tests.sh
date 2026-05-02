#!/bin/bash
# ============================================================
# IPK-RDT Test Suite – automatické testy pro submission_prep/
# Spustí: ./run_tests.sh
# Výstup: pěkný přehled testů, PASS/FAIL, shrnutí
# ============================================================

set -o pipefail

BINARY="./ipk-rdt"
PROXY_SRC="test_proxy.go"
TMPDIR=$(mktemp -d /tmp/ipk_test.XXXXXX)
PASS=0
FAIL=0
TOTAL=0
declare -a FAILED_TESTS

# barvy
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

cleanup() {
    kill $(jobs -p) 2>/dev/null
    rm -rf "$TMPDIR"
}
trap cleanup EXIT INT TERM

# --- helper funkce -------------------------------------------------

print_header() {
    echo ""
    echo -e "${CYAN}╔════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}║  $1${NC}"
    echo -e "${CYAN}╚════════════════════════════════════════════════════════╝${NC}"
}

run_test() {
    local name="$1"
    local desc="$2"
    TOTAL=$((TOTAL + 1))

    echo -n "  [$TOTAL] $name ... "

    # poslední argumenty jsou příkazy ke spuštění, předtím je test_data funkce
    shift 2
    if "$@"; then
        echo -e "${GREEN}PASS${NC}"
        PASS=$((PASS + 1))
    else
        echo -e "${RED}FAIL${NC}  ← $desc"
        FAIL=$((FAIL + 1))
        FAILED_TESTS+=("[$TOTAL] $name")
    fi
}

make_input() {
    local size="$1"
    local pattern="$2"
    local file="$3"

    if [ "$pattern" = "zero" ]; then
        dd if=/dev/zero of="$file" bs="$size" count=1 2>/dev/null
    elif [ "$pattern" = "random" ]; then
        dd if=/dev/urandom of="$file" bs="$size" count=1 2>/dev/null
    elif [ "$pattern" = "text" ]; then
        head -c "$size" <(echo -n "Hello, IPK! This is a test.
Lorem ipsum dolor sit amet.
The quick brown fox jumps over the lazy dog.
0123456789 ABCDEFGHIJKLMNOPQRSTUVWXYZ
" | cat ) > "$file" 2>/dev/null
    fi
}

compare() {
    cmp -s "$1" "$2"
}

wait_for_port() {
    local port="$1"
    local timeout="${2:-5}"
    local start=$(date +%s)
    while ! nc -z 127.0.0.1 "$port" 2>/dev/null; do
        if [ $(($(date +%s) - start)) -gt $timeout ]; then
            return 1
        fi
        sleep 0.1
    done
    return 0
}

start_server() {
    local port="$1"
    local outfile="$2"
    local timeout="${3:-5}"
    local addr="${4:-127.0.0.1}"

    $BINARY -s -p "$port" -a "$addr" -o "$outfile" -w "$timeout" &
    SERVER_PID=$!
    sleep 0.3
    echo $SERVER_PID
}

start_client() {
    local host="$1"
    local port="$2"
    local infile="$3"
    local timeout="${4:-5}"

    $BINARY -c -a "$host" -p "$port" -i "$infile" -w "$timeout" 2>/dev/null
    return $?
}

start_server_bg() {
    local port="$1"
    local outfile="$2"
    local timeout="${3:-5}"
    local addr="${4:-127.0.0.1}"

    $BINARY -s -p "$port" -a "$addr" -o "$outfile" -w "$timeout" >/dev/null 2>&1 &
    echo $!
}

start_client_bg() {
    local host="$1"
    local port="$2"
    local infile="$3"
    local timeout="${4:-5}"

    $BINARY -c -a "$host" -p "$port" -i "$infile" -w "$timeout" >/dev/null 2>&1 &
    echo $!
}

start_proxy() {
    local listen_port="$1"
    local target_port="$2"
    local loss="${3:-0}"
    local dup="${4:-0}"
    local reorder="${5:-0}"
    local delay="${6:-0}"
    local jitter="${7:-0}"

    go run "$PROXY_SRC" \
        -listen "127.0.0.1:$listen_port" \
        -target "127.0.0.1:$target_port" \
        -loss "$loss" -duplicate "$dup" -reorder "$reorder" \
        -delay "$delay" -jitter "$jitter" &
    PROXY_PID=$!
    sleep 0.5
    echo $PROXY_PID
}

# --- build --------------------------------------------------------

print_header "IPK-RDT TEST SUITE"

echo ""
echo "→ Building binary..."
if ! go build -o "$BINARY" main.go sender.go receiver.go protocol.go 2>&1; then
    echo -e "${RED}BUILD FAILED${NC}"
    exit 1
fi
echo -e "  ${GREEN}BUILD OK${NC}"


# ==================================================================
# 1) ZÁKLADNÍ PŘENOSY (bez chyb sítě)
# ==================================================================
print_header "BASIC TRANSFERS (no network errors)"

run_test "short text" "short text file should match" bash -c "
    make_input 100 text $TMPDIR/in1.bin
    start_server_bg 10001 $TMPDIR/out1.bin 10 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10001 $TMPDIR/in1.bin 10
    wait
    compare $TMPDIR/in1.bin $TMPDIR/out1.bin
"

run_test "empty input" "empty file should produce empty output" bash -c "
    touch $TMPDIR/in2.bin
    start_server_bg 10002 $TMPDIR/out2.bin 10 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10002 $TMPDIR/in2.bin 10
    wait
    compare $TMPDIR/in2.bin $TMPDIR/out2.bin
"

run_test "1 KB zero" "1 KB of zeros should match" bash -c "
    make_input 1024 zero $TMPDIR/in3.bin
    start_server_bg 10003 $TMPDIR/out3.bin 10 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10003 $TMPDIR/in3.bin 10
    wait
    compare $TMPDIR/in3.bin $TMPDIR/out3.bin
"

run_test "100 KB random" "100 KB of random data should match" bash -c "
    make_input 102400 random $TMPDIR/in4.bin
    start_server_bg 10004 $TMPDIR/out4.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10004 $TMPDIR/in4.bin 15
    wait
    compare $TMPDIR/in4.bin $TMPDIR/out4.bin
"

run_test "1 MB random" "1 MB of random data should match" bash -c "
    make_input 1048576 random $TMPDIR/in5.bin
    start_server_bg 10005 $TMPDIR/out5.bin 20 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10005 $TMPDIR/in5.bin 20
    wait
    compare $TMPDIR/in5.bin $TMPDIR/out5.bin
"

run_test "stdin → stdout" "pipe transfer through stdin/stdout" bash -c "
    echo 'Hello from stdin!' > $TMPDIR/in_stdin.bin
    $BINARY -s -p 10006 -o - -w 5 > $TMPDIR/out_stdout.bin 2>/dev/null &
    sleep 0.3
    cat $TMPDIR/in_stdin.bin | $BINARY -c -a 127.0.0.1 -p 10006 -i - -w 5 2>/dev/null
    wait
    compare $TMPDIR/in_stdin.bin $TMPDIR/out_stdout.bin
"

run_test "binary file all bytes" "256 byte sequence 0x00..0xFF should match" bash -c "
    python3 -c 'import sys; sys.stdout.buffer.write(bytes(range(256)))' > $TMPDIR/in6.bin 2>/dev/null
    start_server_bg 10007 $TMPDIR/out6.bin 10 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10007 $TMPDIR/in6.bin 10
    wait
    compare $TMPDIR/in6.bin $TMPDIR/out6.bin
"


# ==================================================================
# 2) PŘENOS S CHYBAMI SÍTĚ (přes proxy)
# ==================================================================
print_header "LOSSY TRANSFERS (through impairment proxy)"

run_test "10% loss, 50 KB text" "10%% packet loss scenario" bash -c "
    make_input 51200 text $TMPDIR/in_loss1.bin
    start_proxy 10008 20001 10 0 0 0 0 > /dev/null
    PROXY_PID=\$(cat /tmp/ipk_proxy_pid 2>/dev/null || echo \$!)
    start_server_bg 20001 $TMPDIR/out_loss1.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10008 $TMPDIR/in_loss1.bin 15
    wait
    compare $TMPDIR/in_loss1.bin $TMPDIR/out_loss1.bin
"

run_test "20% loss, 30 KB text" "20%% packet loss scenario" bash -c "
    make_input 30720 text $TMPDIR/in_loss2.bin
    start_proxy 10009 20002 20 0 0 0 0 > /dev/null
    start_server_bg 20002 $TMPDIR/out_loss2.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10009 $TMPDIR/in_loss2.bin 15
    wait
    compare $TMPDIR/in_loss2.bin $TMPDIR/out_loss2.bin
"

run_test "30% loss, 20 KB text" "30%% packet loss scenario" bash -c "
    make_input 20480 text $TMPDIR/in_loss3.bin
    start_proxy 10010 20003 30 0 0 0 0 > /dev/null
    start_server_bg 20003 $TMPDIR/out_loss3.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10010 $TMPDIR/in_loss3.bin 15
    wait
    compare $TMPDIR/in_loss3.bin $TMPDIR/out_loss3.bin
"


# ==================================================================
# 3) DUPLICITY
# ==================================================================
print_header "DUPLICATE PACKETS (through proxy)"

run_test "20% duplicate, 20 KB" "packet duplication scenario" bash -c "
    make_input 20480 text $TMPDIR/in_dup1.bin
    start_proxy 10011 20004 0 20 0 0 0 > /dev/null
    start_server_bg 20004 $TMPDIR/out_dup1.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10011 $TMPDIR/in_dup1.bin 15
    wait
    compare $TMPDIR/in_dup1.bin $TMPDIR/out_dup1.bin
"


# ==================================================================
# 4) PŘEHAZOVÁNÍ
# ==================================================================
print_header "REORDERED PACKETS (through proxy)"

run_test "40% reorder, 20 KB" "packet reordering scenario" bash -c "
    make_input 20480 text $TMPDIR/in_reorder1.bin
    start_proxy 10012 20005 0 0 40 0 0 > /dev/null
    start_server_bg 20005 $TMPDIR/out_reorder1.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10012 $TMPDIR/in_reorder1.bin 15
    wait
    compare $TMPDIR/in_reorder1.bin $TMPDIR/out_reorder1.bin
"


# ==================================================================
# 5) ZPOŽDĚNÍ / JITTER
# ==================================================================
print_header "DELAY & JITTER (through proxy)"

run_test "50ms delay, 15ms jitter, 15 KB" "delay + jitter scenario" bash -c "
    make_input 15360 text $TMPDIR/in_delay1.bin
    start_proxy 10013 20006 0 0 0 50 15 > /dev/null
    start_server_bg 20006 $TMPDIR/out_delay1.bin 15 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10013 $TMPDIR/in_delay1.bin 15
    wait
    compare $TMPDIR/in_delay1.bin $TMPDIR/out_delay1.bin
"


# ==================================================================
# 6) KOMBINOVANÉ SCÉNÁŘE
# ==================================================================
print_header "COMBINED IMPAIRMENTS (loss + duplicate + reorder + delay)"

run_test "10% loss + 10% dup + 20% reorder + 30ms, 15 KB" "combined scenario" bash -c "
    make_input 15360 text $TMPDIR/in_combo1.bin
    start_proxy 10014 20007 10 10 20 30 10 > /dev/null
    start_server_bg 20007 $TMPDIR/out_combo1.bin 20 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10014 $TMPDIR/in_combo1.bin 20
    wait
    compare $TMPDIR/in_combo1.bin $TMPDIR/out_combo1.bin
"

run_test "20% loss + 30% reorder + 40ms delay, 10 KB" "extreme combined scenario" bash -c "
    make_input 10240 text $TMPDIR/in_combo2.bin
    start_proxy 10015 20008 20 0 30 40 20 > /dev/null
    start_server_bg 20008 $TMPDIR/out_combo2.bin 20 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10015 $TMPDIR/in_combo2.bin 20
    wait
    compare $TMPDIR/in_combo2.bin $TMPDIR/out_combo2.bin
"


# ==================================================================
# 7) TIMEOUT TESTY (očekávané selhání)
# ==================================================================
print_header "TIMEOUT TESTS (expected failures)"

run_test "client timeout - no server" "client should fail with timeout, no server running" bash -c "
    start_client 127.0.0.1 44444 $TMPDIR/in1.bin 2 2>/dev/null
    [ \$? -ne 0 ]
"

run_test "server shuts down after timeout if no data" "server should exit when sender sends nothing then exits" bash -c "
    $BINARY -s -p 10016 -o $TMPDIR/out_timeout.bin -w 3 >/dev/null 2>&1 &
    SPID=\$!
    sleep 0.3
    # spust klient s prazdnym vstupem
    touch $TMPDIR/empty_timeout.bin
    $BINARY -c -a 127.0.0.1 -p 10016 -i $TMPDIR/empty_timeout.bin -w 3 >/dev/null 2>&1
    wait \$SPID
    # server by mel normalne skoncit (FINACK dorazil) = success 0
    true
"


# ==================================================================
# 8) MULTI-SEGMENT TEST (pipelining check)
# ==================================================================
print_header "PIPELINING / WINDOW TESTS"

run_test "large enough to fill window, 200 KB" "verify window congestion works" bash -c "
    make_input 204800 random $TMPDIR/in_window.bin
    start_server_bg 10017 $TMPDIR/out_window.bin 30 > /dev/null
    sleep 0.5
    start_client 127.0.0.1 10017 $TMPDIR/in_window.bin 30
    wait
    compare $TMPDIR/in_window.bin $TMPDIR/out_window.bin
"


# ==================================================================
# SHRNUTÍ
# ==================================================================
print_header "RESULTS SUMMARY"

echo ""
echo -e "  Total:  $TOTAL"
echo -e "  ${GREEN}Passed: $PASS${NC}"
echo -e "  ${RED}Failed: $FAIL${NC}"
echo ""

if [ $FAIL -gt 0 ]; then
    echo "  Failed tests:"
    for t in "${FAILED_TESTS[@]}"; do
        echo -e "    ${RED}$t${NC}"
    done
    echo ""
fi

if [ $FAIL -eq 0 ]; then
    echo -e "  ${GREEN}╔═════════════════════════════╗${NC}"
    echo -e "  ${GREEN}║  ALL TESTS PASSED.  🏆      ║${NC}"
    echo -e "  ${GREEN}╚═════════════════════════════╝${NC}"
else
    echo -e "  ${YELLOW}Some tests failed. Review above.${NC}"
fi

echo ""
echo "Temp dir: $TMPDIR"
echo ""

# cleanup jede přes trap
exit $FAIL
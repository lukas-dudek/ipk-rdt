#!/bin/sh
# ============================================================
# IPK-RDT test runner – POSIX sh, subshell + watchdog
# ============================================================
set -eu

TMPDIR=$(mktemp -d /tmp/ipk.XXXX)
trap 'rm -rf "$TMPDIR"; kill 0 2>/dev/null' EXIT INT TERM

G=$(printf '\033[32m')
R=$(printf '\033[31m')
C=$(printf '\033[36m')
N=$(printf '\033[0m')

pass=0 fail=0 total=0
TEST_TIMEOUT=40

header() {
    printf '\n%s=== %s ===%s\n' "$C" "$1" "$N"
}

# ---------------------------------------------------------
# Watchdog – spustí příkaz v subshellu, hlídá timeout
# ---------------------------------------------------------
run_with_timeout() {
    _wt_tout="$1"; shift

    # spustit hlavní kód v subshellu (zdědí funkce!)
    ( "$@") &
    _wt_pid=$!

    # watchdog: po timeoutu zabije testovací subshell
    ( sleep "$_wt_tout"; kill $_wt_pid 2>/dev/null) &
    _wt_wdog=$!

    wait $_wt_pid 2>/dev/null
    _wt_ret=$?

    # uklidit watchdog
    kill $_wt_wdog 2>/dev/null
    wait $_wt_wdog 2>/dev/null

    return $_wt_ret
}

# ---------------------------------------------------------
# Testovací infra
# ---------------------------------------------------------
test_one() {
    _tn_name="$1"; shift
    total=$((total+1))
    printf '  [%2d] %-50s ... ' "$total" "$_tn_name"
    _tn_start=$(date +%s)
    _tn_out="$TMPDIR/out"
    if run_with_timeout "$TEST_TIMEOUT" "$@" >"$_tn_out" 2>&1; then
        _tn_t=$(($(date +%s)-_tn_start))
        printf '%sPASS%s (%ss)\n' "$G" "$N" "$_tn_t"
        pass=$((pass+1))
    else
        _tn_t=$(($(date +%s)-_tn_start))
        printf '%sFAIL%s (%ss)\n' "$R" "$N" "$_tn_t"
        tail -6 "$_tn_out" | while IFS= read -r l; do printf '        | %s\n' "$l"; done
        fail=$((fail+1))
    fi
}

# ---------------------------------------------------------
header "BUILD"
go build -o ipk-rdt . 2>&1
echo "  Binary ready."

# ---------------------------------------------------------
# Generátory a nástroje (bez local, žádné export)
# ---------------------------------------------------------

mktxt() {
    _mkt_sz="$1" _mkt_f="$2"
    _mkt_blk="Hello IPK reliable test. 0123456789 ABCDEF. The quick brown fox. "
    _mkt_bl=${#_mkt_blk}
    :>"$_mkt_f"
    _mkt_w=0
    while [ "$_mkt_w" -lt "$_mkt_sz" ]; do
        _mkt_r=$((_mkt_sz-_mkt_w))
        if [ "$_mkt_r" -ge "$_mkt_bl" ]; then
            printf '%s' "$_mkt_blk" >>"$_mkt_f"
            _mkt_w=$((_mkt_w+_mkt_bl))
        else
            printf "%.${_mkt_r}s" "$_mkt_blk" >>"$_mkt_f"
            _mkt_w="$_mkt_sz"
        fi
    done
}

mkzero() { dd if=/dev/zero of="$2" bs="$1" count=1 2>/dev/null; }
mkrand() { dd if=/dev/urandom of="$2" bs="$1" count=1 2>/dev/null; }

mkall() {
    _ml_f="$1"
    :>"$_ml_f"
    _ml_i=0
    while [ "$_ml_i" -le 255 ]; do
        printf "\\$(printf '%03o' "$_ml_i")" >>"$_ml_f"
        _ml_i=$((_ml_i+1))
    done
}

srv() { ./ipk-rdt -s -p "$1" -a 127.0.0.1 -o "$2" -w "$3" >/dev/null 2>&1; }
cli() { ./ipk-rdt -c -a 127.0.0.1 -p "$1" -i "$2" -w "$3" >/dev/null 2>&1; }

proxy() {
    go run test_proxy.go \
        -listen "127.0.0.1:$1" \
        -target "127.0.0.1:$2" \
        -loss "${3:-0}" -duplicate "${4:-0}" -reorder "${5:-0}" \
        -delay "${6:-0}" -jitter "${7:-0}" >/dev/null 2>&1 &
    sleep 0.4
}

# ---------------------------------------------------------
# Testovací funkce (volány přímo, ne přes sh -c)
# ---------------------------------------------------------

test_basic() {
    _tb_nm="$1" _tb_sz="$2" _tb_gen="$3" _tb_to="$4" _tb_port="$5"
    _tb_in="$TMPDIR/$_tb_nm.in" _tb_out="$TMPDIR/$_tb_nm.out"
    $_tb_gen "$_tb_sz" "$_tb_in"
    srv "$_tb_port" "$_tb_out" "$_tb_to" &
    _tb_spid=$!
    sleep 0.2
    cli "$_tb_port" "$_tb_in" "$_tb_to"
    wait $_tb_spid
    cmp -s "$_tb_in" "$_tb_out"
}

test_proxied() {
    _tp_nm="$1" _tp_sz="$2" _tp_gen="$3" _tp_to="$4"
    _pp="$5" _tpp="$6" _l="$7" _d="$8" _r="$9" _dl="${10}" _ji="${11}"
    _tp_in="$TMPDIR/$_tp_nm.in" _tp_out="$TMPDIR/$_tp_nm.out"
    $_tp_gen "$_tp_sz" "$_tp_in"
    proxy "$_pp" "$_tpp" "$_l" "$_d" "$_r" "$_dl" "$_ji"
    _tp_xpid=$!
    srv "$_tpp" "$_tp_out" "$_tp_to" &
    _tp_spid=$!
    sleep 0.2
    cli "$_pp" "$_tp_in" "$_tp_to"
    wait $_tp_spid; _tp_ok=$?
    kill $_tp_xpid 2>/dev/null; wait $_tp_xpid 2>/dev/null
    [ $_tp_ok -eq 0 ] && cmp -s "$_tp_in" "$_tp_out"
}

# ---------------------------------------------------------
# 1) BASIC TRANSFERS
# ---------------------------------------------------------
header "BASIC"

test_one "Short text (500B)" test_basic "st" 500 mktxt 5 10001
test_one "Empty input" test_basic "empty" 0 mktxt 5 10002
test_one "1 KB zeros" test_basic "1kz" 1024 mkzero 5 10003
test_one "100 KB text" test_basic "100k" 102400 mktxt 15 10004
test_one "1 MB random" test_basic "1mb" 1048576 mkrand 30 10005
test_one "All bytes 0x00..0xFF" test_basic "allb" 256 mkall 5 10007

test_one "Stdin -> stdout" sh -c '
    base="$TMPDIR"
    echo "Hello stdin test 12345" > "$base/std.in"
    ./ipk-rdt -s -p 10006 -o - -w 5 > "$base/std.out" 2>/dev/null &
    spid=$!
    sleep 0.2
    cat "$base/std.in" | ./ipk-rdt -c -a 127.0.0.1 -p 10006 -i - -w 5 2>/dev/null
    wait $spid
    cmp -s "$base/std.in" "$base/std.out"
'

# ---------------------------------------------------------
# 2) IMPAIRMENTY
# ---------------------------------------------------------
header "LOSS / DUP / REORDER / DELAY"

test_one "10% loss, 50 KB" test_proxied "l10" 51200 mktxt 20 10008 20001 10 0 0 0 0
test_one "20% loss, 30 KB" test_proxied "l20" 30720 mktxt 20 10009 20002 20 0 0 0 0
test_one "30% loss, 20 KB" test_proxied "l30" 20480 mktxt 20 10010 20003 30 0 0 0 0
test_one "20% duplicates, 20 KB" test_proxied "dup" 20480 mktxt 20 10011 20004 0 20 0 0 0
test_one "40% reorder, 20 KB" test_proxied "reo" 20480 mktxt 20 10012 20005 0 0 40 0 0
test_one "50ms delay + 15ms jitter, 15 KB" test_proxied "del" 15360 mktxt 25 10013 20006 0 0 0 50 15
test_one "Combo moderate, 15 KB" test_proxied "comb" 15360 mktxt 30 10014 20007 10 10 20 30 10
test_one "Combo extreme, 10 KB" test_proxied "combx" 10240 mktxt 30 10015 20008 20 0 30 40 20

# ---------------------------------------------------------
# 3) TIMEOUT
# ---------------------------------------------------------
header "TIMEOUT"

test_one "Client timeout (no server)" sh -c '
    ./ipk-rdt -c -a 127.0.0.1 -p 55555 -i /dev/null -w 2 >/dev/null 2>&1
    [ $? -ne 0 ]
'

test_one "Empty input teardown" sh -c '
    base="$TMPDIR"
    touch "$base/et.in"
    ./ipk-rdt -s -p 10016 -o "$base/et.out" -w 5 >/dev/null 2>&1 &
    spid=$!
    sleep 0.2
    ./ipk-rdt -c -a 127.0.0.1 -p 10016 -i "$base/et.in" -w 5 >/dev/null 2>&1
    wait $spid
    cmp -s "$base/et.in" "$base/et.out"
'

# ---------------------------------------------------------
# 4) WINDOW
# ---------------------------------------------------------
header "WINDOW"

test_one "200 KB random (window)" sh -c '
    base="$TMPDIR"
    dd if=/dev/urandom of="$base/win.in" bs=204800 count=1 2>/dev/null
    ./ipk-rdt -s -p 10017 -o "$base/win.out" -w 40 >/dev/null 2>&1 &
    spid=$!
    sleep 0.2
    ./ipk-rdt -c -a 127.0.0.1 -p 10017 -i "$base/win.in" -w 40 >/dev/null 2>&1
    wait $spid
    cmp -s "$base/win.in" "$base/win.out"
'

# ---------------------------------------------------------
# RESULTS
# ---------------------------------------------------------
header "RESULTS"
echo ""
echo "  Total:  $total"
printf '  %sPassed: %s%s\n' "$G" "$pass" "$N"
printf '  %sFailed: %s%s\n' "$R" "$fail" "$N"
echo ""
if [ "$fail" -eq 0 ]; then
    printf '%s  ALL TESTS PASSED  %s\n' "$G" "$N"
else
    printf '%s  Some failed  %s\n' "$C" "$N"
fi

exit $fail
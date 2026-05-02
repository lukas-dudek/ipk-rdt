#!/bin/sh
# ============================================================
# IPK-RDT POSIX test runner – opraveno AllBytes, stdin, proxy
# ============================================================
set -eu

TMPDIR=$(mktemp -d /tmp/ipk.XXXX)
# uklidit bez kill 0
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

G=$(printf '\033[32m')
R=$(printf '\033[31m')
C=$(printf '\033[36m')
N=$(printf '\033[0m')

pass=0 fail=0 total=0
TEST_TIMEOUT=40

header() { printf '\n%s=== %s ===%s\n' "$C" "$1" "$N"; }

# ---------------------------------------------------------
# Watchdog – spustí subshell, po timeoutu ho zabije
# ---------------------------------------------------------
run_with_timeout() {
    _wt_tout="$1"; shift
    ( "$@" ) &
    _wt_pid=$!
    ( sleep "$_wt_tout"; kill $_wt_pid 2>/dev/null ) &
    _wt_wdog=$!
    wait $_wt_pid 2>/dev/null
    _wt_ret=$?
    kill $_wt_wdog 2>/dev/null
    wait $_wt_wdog 2>/dev/null
    return $_wt_ret
}

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
# Generátory – opraveno mkall (přijímá [velikost] soubor)
# ---------------------------------------------------------
mktxt() {
    _s="$1" _f="$2"
    blk="Hello IPK reliable test. 0123456789 ABCDEF. The quick brown fox. "
    bl=${#blk}
    :>"$_f"
    w=0
    while [ "$w" -lt "$_s" ]; do
        r=$((_s-w))
        if [ "$r" -ge "$bl" ]; then
            printf '%s' "$blk" >>"$_f"
            w=$((w+bl))
        else
            printf "%.${r}s" "$blk" >>"$_f"
            w="$_s"
        fi
    done
}

mkzero() { dd if=/dev/zero of="$2" bs="$1" count=1 2>/dev/null; }
mkrand() { dd if=/dev/urandom of="$2" bs="$1" count=1 2>/dev/null; }

mkall() {
    # akceptuje volání: mkall soubor  NEBO  mkall velikost soubor
    if [ $# -ge 2 ]; then _f="$2"; else _f="$1"; fi
    :>"$_f"
    i=0
    while [ "$i" -le 255 ]; do
        printf "\\$(printf '%03o' "$i")" >>"$_f"
        i=$((i+1))
    done
}

srv() { ./ipk-rdt -s -p "$1" -a 127.0.0.1 -o "$2" -w "$3" >/dev/null 2>&1; }
cli() { ./ipk-rdt -c -a 127.0.0.1 -p "$1" -i "$2" -w "$3" >/dev/null 2>&1; }

proxy() {
    # go binárka nalezena při BUILD, jinak fail
    go run test_proxy.go \
        -listen "127.0.0.1:$1" -target "127.0.0.1:$2" \
        -loss "${3:-0}" -duplicate "${4:-0}" -reorder "${5:-0}" \
        -delay "${6:-0}" -jitter "${7:-0}" >/dev/null 2>&1 &
}

# ---------------------------------------------------------
# Testovací akce
# ---------------------------------------------------------
test_basic() {
    _tb_nm="$1" _tb_sz="$2" _tb_gen="$3" _tb_to="$4" _tb_port="$5"
    in="$TMPDIR/$_tb_nm.in" out="$TMPDIR/$_tb_nm.out"
    $_tb_gen "$_tb_sz" "$in"
    srv "$_tb_port" "$out" "$_tb_to" &
    spid=$!
    sleep 0.2
    cli "$_tb_port" "$in" "$_tb_to"
    wait $spid
    cmp -s "$in" "$out"
}

test_proxied() {
    nm="$1" sz="$2" gen="$3" to="$4"
    pp="$5" tp="$6" l="$7" d="$8" r="$9" dl="${10}" ji="${11}"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    # spustit proxy s úklidem při ukončení subshellu
    proxy "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
    xpid=$!
    trap 'kill $xpid 2>/dev/null; wait $xpid 2>/dev/null' EXIT
    srv "$tp" "$out" "$to" &
    spid=$!
    sleep 0.2
    cli "$pp" "$in" "$to"
    wait $spid; ok=$?
    kill $xpid 2>/dev/null; wait $xpid 2>/dev/null
    trap - EXIT
    [ $ok -eq 0 ] && cmp -s "$in" "$out"
}

# ---------------------------------------------------------
# 1) BASIC
# ---------------------------------------------------------
header "BASIC"

test_one "Short text (500B)" test_basic "st" 500 mktxt 5 10001
test_one "Empty input" test_basic "empty" 0 mktxt 5 10002
test_one "1 KB zeros" test_basic "1kz" 1024 mkzero 5 10003
test_one "100 KB text" test_basic "100k" 102400 mktxt 15 10004
test_one "1 MB random" test_basic "1mb" 1048576 mkrand 30 10005
test_one "All bytes 0x00..0xFF" test_basic "allb" 256 mkall 5 10007

# Stdin/stdout test – opraveno, server stdout přesměrován do souboru
test_one "Stdin -> stdout" sh -c '
    base="$TMPDIR"
    echo "Hello stdin test 12345" > "$base/std.in"
    # server: stdout -> "$base/std.out", stderr -> /dev/null
    ./ipk-rdt -s -p 10006 -o - -w 5 > "$base/std.out" 2>/dev/null &
    spid=$!
    sleep 0.2
    cat "$base/std.in" | ./ipk-rdt -c -a 127.0.0.1 -p 10006 -i - -w 5 2>/dev/null
    wait $spid
    if ! cmp -s "$base/std.in" "$base/std.out"; then
        # debug info
        wc -c "$base/std.in" "$base/std.out"
        od -c "$base/std.out" | head -1
        return 1
    fi
'

# ---------------------------------------------------------
# 2) IMPAIRMENTY – jen pokud existuje test_proxy.go
# ---------------------------------------------------------
if [ -f test_proxy.go ]; then
    header "LOSS / DUP / REORDER / DELAY"

    test_one "10% loss, 50 KB" test_proxied "l10" 51200 mktxt 20 10008 20001 10 0 0 0 0
    test_one "20% loss, 30 KB" test_proxied "l20" 30720 mktxt 20 10009 20002 20 0 0 0 0
    test_one "30% loss, 20 KB" test_proxied "l30" 20480 mktxt 20 10010 20003 30 0 0 0 0
    test_one "20% duplicates, 20 KB" test_proxied "dup" 20480 mktxt 20 10011 20004 0 20 0 0 0
    test_one "40% reorder, 20 KB" test_proxied "reo" 20480 mktxt 20 10012 20005 0 0 40 0 0
    test_one "50ms delay + 15ms jitter, 15 KB" test_proxied "del" 15360 mktxt 25 10013 20006 0 0 0 50 15
    test_one "Combo moderate, 15 KB" test_proxied "comb" 15360 mktxt 30 10014 20007 10 10 20 30 10
    test_one "Combo extreme, 10 KB" test_proxied "combx" 10240 mktxt 30 10015 20008 20 0 30 40 20
else
    header "LOSS / DUP / REORDER / DELAY (SKIPPED – test_proxy.go missing)"
fi

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
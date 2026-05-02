#!/bin/sh
# ============================================================
# IPK-RDT test runner – POSIX sh, vlastní watchdog
# ============================================================
set -eu

TMPDIR=$(mktemp -d /tmp/ipk.XXXX)
# shellcheck disable=SC2064
trap "rm -rf $TMPDIR; kill 0 2>/dev/null" EXIT INT TERM

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
# Jednoduchý watchdog – spusť příkaz na pozadí, čekej max N sekund
# ---------------------------------------------------------
run_with_timeout() {
    # $1 = timeout, zbytek = příkaz
    _wt_tout="$1"; shift

    # spusť test na pozadí
    "$@" &
    _wt_pid=$!

    # watchdog: po timeoutu test zabije
    ( sleep "$_wt_tout"; kill $_wt_pid 2>/dev/null ) &
    _wt_wdog=$!

    wait $_wt_pid 2>/dev/null
    _wt_ret=$?

    # uklid watchdog (mohl už doběhnout, nebo ne)
    kill $_wt_wdog 2>/dev/null
    wait $_wt_wdog 2>/dev/null

    return $_wt_ret
}

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
# Generátory, server, klient, proxy – bez 'local'
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
# Test actions (shell kód, ne funkce)
# ---------------------------------------------------------

basic_action='{ # akce basic
    nm="$1" sz="$2" gen="$3" timeout="$4" port="$5"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    srv "$port" "$out" "$timeout" &
    spid=$!
    sleep 0.2
    cli "$port" "$in" "$timeout"
    wait $spid
    cmp -s "$in" "$out"
}'

proxied_action='{ # akce proxied
    nm="$1" sz="$2" gen="$3" timeout="$4" pp="$5" tp="$6" l="$7" d="$8" r="$9" dl="${10}" ji="${11}"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    proxy "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
    xpid=$!
    srv "$tp" "$out" "$timeout" &
    spid=$!
    sleep 0.2
    cli "$pp" "$in" "$timeout"
    wait $spid; ok=$?
    kill $xpid 2>/dev/null; wait $xpid 2>/dev/null
    [ $ok -eq 0 ] && cmp -s "$in" "$out"
}'

# ---------------------------------------------------------
# 1) BASIC
# ---------------------------------------------------------
header "BASIC"

test_one "Short_text_500B" sh -c "$basic_action" _ "st" 500 mktxt 5 10001
test_one "Empty_input" sh -c "$basic_action" _ "empty" 0 mktxt 5 10002
test_one "1KB_zeros" sh -c "$basic_action" _ "1kz" 1024 mkzero 5 10003
test_one "100KB_text" sh -c "$basic_action" _ "100k" 102400 mktxt 15 10004
test_one "1MB_random" sh -c "$basic_action" _ "1mb" 1048576 mkrand 30 10005
test_one "All_bytes_0-255" sh -c "
    in=\"\$TMPDIR/allb.in\" out=\"\$TMPDIR/allb.out\"
    mkall \"\$in\"
    srv 10007 \"\$out\" 5 &
    spid=\$!
    sleep 0.2
    cli 10007 \"\$in\" 5
    wait \$spid
    cmp -s \"\$in\" \"\$out\"
"

test_one "Stdin->stdout" sh -c '
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
# 2) IMPAIRMENTS
# ---------------------------------------------------------
header "LOSS / DUP / REORDER / DELAY"

test_one "10%_loss_50KB" sh -c "$proxied_action" _ "l10" 51200 mktxt 20 10008 20001 10 0 0 0 0
test_one "20%_loss_30KB" sh -c "$proxied_action" _ "l20" 30720 mktxt 20 10009 20002 20 0 0 0 0
test_one "30%_loss_20KB" sh -c "$proxied_action" _ "l30" 20480 mktxt 20 10010 20003 30 0 0 0 0
test_one "20%_dup_20KB" sh -c "$proxied_action" _ "dup" 20480 mktxt 20 10011 20004 0 20 0 0 0
test_one "40%_reorder_20KB" sh -c "$proxied_action" _ "reo" 20480 mktxt 20 10012 20005 0 0 40 0 0
test_one "50ms_delay_15ms_jitter" sh -c "$proxied_action" _ "del" 15360 mktxt 25 10013 20006 0 0 0 50 15
test_one "Combo_moderate_15KB" sh -c "$proxied_action" _ "comb" 15360 mktxt 30 10014 20007 10 10 20 30 10
test_one "Combo_extreme_10KB" sh -c "$proxied_action" _ "combx" 10240 mktxt 30 10015 20008 20 0 30 40 20

# ---------------------------------------------------------
# 3) TIMEOUT
# ---------------------------------------------------------
header "TIMEOUT"

test_one "Client_timeout_no_server" sh -c '
    ./ipk-rdt -c -a 127.0.0.1 -p 55555 -i /dev/null -w 2 >/dev/null 2>&1
    [ $? -ne 0 ]
'

test_one "Empty_input_teardown" sh -c '
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

test_one "200KB_random_window" sh -c '
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
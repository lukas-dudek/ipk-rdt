#!/bin/sh#
# ============================================================
# IPK-RDT POSIX test runner – funguje v Nix /bin/sh
# Spusť: ./test_runner.sh
# ============================================================
set -eu

TMPDIR=$(mktemp -d /tmp/ipk.XXXX)
# shellcheck disable=SC2064
trap "rm -rf $TMPDIR" EXIT INT TERM

# barvy – printf pro ANSI, ne echo -e
G=$(printf '\033[32m')
R=$(printf '\033[31m')
C=$(printf '\033[36m')
N=$(printf '\033[0m')

pass=0 fail=0 total=0

header() {
    printf '\n%s=== %s ===%s\n' "$C" "$1" "$N"
}

test_one() {
    name="$1"; shift
    total=$((total+1))
    printf '  [%2d] %-50s ... ' "$total" "$name"
    start=$(date +%s)
    out="$TMPDIR/out"
    if "$@" >"$out" 2>&1; then
        t=$(($(date +%s)-start))#!/bin/sh
# ============================================================
# IPK-RDT POSIX test runner V2 – s timeouty a opravou mkall
# ============================================================
set -eu

TMPDIR=$(mktemp -d /tmp/ipk.XXXX)
trap "rm -rf $TMPDIR; kill \$(jobs -p) 2>/dev/null" EXIT INT TERM

G=$(printf '\033[32m')
R=$(printf '\033[31m')
C=$(printf '\033[36m')
N=$(printf '\033[0m')

pass=0 fail=0 total=0
TEST_TIMEOUT=40

header() {
    printf '\n%s=== %s ===%s\n' "$C" "$1" "$N"
}

# úklid před každým testem
cleanup_procs() {
    # zabít zbytky proxy/server z minulých testů
    pkill -f "go run test_proxy.go" 2>/dev/null || true
    pkill -f "./ipk-rdt -s" 2>/dev/null || true
    sleep 0.2
}

test_one() {
    name="$1"; shift
    total=$((total+1))
    printf '  [%2d] %-50s ... ' "$total" "$name"
    start=$(date +%s)
    out="$TMPDIR/out"
    cleanup_procs
    if timeout "$TEST_TIMEOUT" "$@" >"$out" 2>&1; then
        t=$(($(date +%s)-start))
        printf '%sPASS%s (%ss)\n' "$G" "$N" "$t"
        pass=$((pass+1))
    else
        t=$(($(date +%s)-start))
        printf '%sFAIL%s (%ss)\n' "$R" "$N" "$t"
        tail -6 "$out" | while IFS= read -r l; do printf '        | %s\n' "$l"; done
        fail=$((fail+1))
    fi
}

# ---------------------------------------------------------
header "BUILD"
go build -o ipk-rdt . 2>&1
echo "  Binary ready."

# ---------------------------------------------------------
# Generátory
# ---------------------------------------------------------

mktxt() {
    sz="$1" f="$2"
    blk="Hello IPK reliable test. 0123456789 ABCDEF. The quick brown fox. "
    bl=${#blk}
    :>"$f"
    w=0
    while [ "$w" -lt "$sz" ]; do
        r=$((sz-w))
        if [ "$r" -ge "$bl" ]; then
            printf '%s' "$blk" >>"$f"
            w=$((w+bl))
        else
            printf "%.${r}s" "$blk" >>"$f"
            w="$sz"
        fi
    done
}

mkzero() {
    dd if=/dev/zero of="$2" bs="$1" count=1 2>/dev/null
}

mkrand() {
    dd if=/dev/urandom of="$2" bs="$1" count=1 2>/dev/null
}

# OPRAVA: bere velikost (ignoruje) a soubor
mkall() {
    f="${2:-$1}"   # kompatibilita: volání jako mkall 256 file
    :>"$f"
    i=0
    while [ "$i" -le 255 ]; do
        printf "\\$(printf '%03o' "$i")" >>"$f"
        i=$((i+1))
    done
}

srv() {
    ./ipk-rdt -s -p "$1" -a 127.0.0.1 -o "$2" -w "$3" >/dev/null 2>&1
}

cli() {
    ./ipk-rdt -c -a 127.0.0.1 -p "$1" -i "$2" -w "$3" >/dev/null 2>&1
}

proxy() {
    go run test_proxy.go \
        -listen "127.0.0.1:$1" \
        -target "127.0.0.1:$2" \
        -loss "${3:-0}" -duplicate "${4:-0}" -reorder "${5:-0}" \
        -delay "${6:-0}" -jitter "${7:-0}" >/dev/null 2>&1 &
    sleep 0.3
}

test_basic() {
    nm="$1" sz="$2" gen="$3" srvto="$4" port="$5"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    srv "$port" "$out" "$srvto" &
    spid=$!
    sleep 0.2
    cli "$port" "$in" "$srvto"
    wait $spid
    cmp -s "$in" "$out"
}

test_proxied() {
    nm="$1" sz="$2" gen="$3" srvto="$4" pp="$5" tp="$6" l="$7" d="$8" r="$9" dl="${10}" ji="${11}"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    proxy "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
    xpid=$!
    srv "$tp" "$out" "$srvto" &
    spid=$!
    sleep 0.2
    cli "$pp" "$in" "$srvto"
    wait $spid; ok=$?
    kill $xpid 2>/dev/null; wait $xpid 2>/dev/null
    [ $ok -eq 0 ] && cmp -s "$in" "$out"
}

# ---------------------------------------------------------
# 1) BASIC
# ---------------------------------------------------------
header "BASIC"

echo "Short_text_500B 500 mktxt 5 10001
Empty_input 0 mktxt 5 10002
1KB_zeros 1024 mkzero 5 10003
100KB_text 102400 mktxt 15 10004
1MB_random 1048576 mkrand 30 10005
All_bytes_0-255 256 mkall 5 10007" | while read nm sz gen to port; do
    test_one "$nm" test_basic "$nm" "$sz" "$gen" "$to" "$port"
done

test_one "Stdin->stdout" sh -c '
    base='"$TMPDIR"'
    echo "Hello stdin test 12345" > "$base/std.in"
    ./ipk-rdt -s -p 10006 -o - -w 5 > "$base/std.out" 2>/dev/null &
    spid=$!
    sleep 0.2
    cat "$base/std.in" | ./ipk-rdt -c -a 127.0.0.1 -p 10006 -i - -w 5 2>/dev/null
    wait $spid
    # porovnání ignoruje případný trailing newline z echo? Ne, echo přidá \n, klient to pošle, server přijme, mělo by sedět.
    cmp -s "$base/std.in" "$base/std.out"
'

# ---------------------------------------------------------
# 2) IMPAIRMENTS
# ---------------------------------------------------------
header "LOSS / DUP / REORDER / DELAY"

echo "10%_loss_50KB 51200 20 10008 20001 10 0 0 0 0
20%_loss_30KB 30720 20 10009 20002 20 0 0 0 0
30%_loss_20KB 20480 20 10010 20003 30 0 0 0 0
20%_dup_20KB 20480 20 10011 20004 0 20 0 0 0
40%_reorder_20KB 20480 20 10012 20005 0 0 40 0 0
50ms_delay_15ms_jitter_15KB 15360 25 10013 20006 0 0 0 50 15
Combo_moderate_15KB 15360 30 10014 20007 10 10 20 30 10
Combo_extreme_10KB 10240 30 10015 20008 20 0 30 40 20" | while read nm sz srvto pp tp l d r dl ji; do
    test_one "$nm" test_proxied "$nm" "$sz" mktxt "$srvto" "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
done

# ---------------------------------------------------------
# 3) TIMEOUT
# ---------------------------------------------------------
header "TIMEOUT"

test_one "Client_timeout_no_server" sh -c '
    ./ipk-rdt -c -a 127.0.0.1 -p 55555 -i /dev/null -w 2 >/dev/null 2>&1
    [ $? -ne 0 ]
'

test_one "Empty_input_teardown" sh -c '
    base='"$TMPDIR"'
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
    base='"$TMPDIR"'
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
        printf '%sPASS%s (%ss)\n' "$G" "$N" "$t"
        pass=$((pass+1))
    else
        t=$(($(date +%s)-start))
        printf '%sFAIL%s (%ss)\n' "$R" "$N" "$t"
        tail -3 "$out" | while read -r l; do echo "        | $l"; done
        fail=$((fail+1))
    fi
}

# ---------------------------------------------------------
header "BUILD"
go build -o ipk-rdt . 2>&1
echo "  Binary ready."

# ---------------------------------------------------------
# Pomocné funkce
# ---------------------------------------------------------

mktxt() {
    sz="$1" f="$2"
    blk="Hello IPK reliable test. 0123456789 ABCDEF. The quick brown fox. "
    bl=${#blk}
    :>"$f"
    w=0
    while [ "$w" -lt "$sz" ]; do
        r=$((sz-w))
        if [ "$r" -ge "$bl" ]; then
            printf '%s' "$blk" >>"$f"
            w=$((w+bl))
        else
            printf "%.${r}s" "$blk" >>"$f"
            w="$sz"
        fi
    done
}

mkzero() {
    dd if=/dev/zero of="$2" bs="$1" count=1 2>/dev/null
}

mkrand() {
    dd if=/dev/urandom of="$2" bs="$1" count=1 2>/dev/null
}

mkall() {
    :>"$1"
    i=0
    while [ "$i" -le 255 ]; do
        printf "\\$(printf '%03o' "$i")" >>"$1"
        i=$((i+1))
    done
}

srv() {
    ./ipk-rdt -s -p "$1" -a 127.0.0.1 -o "$2" -w "$3" >/dev/null 2>&1
}

cli() {
    ./ipk-rdt -c -a 127.0.0.1 -p "$1" -i "$2" -w "$3" >/dev/null 2>&1
}

proxy() {
    go run test_proxy.go \
        -listen "127.0.0.1:$1" \
        -target "127.0.0.1:$2" \
        -loss "${3:-0}" -duplicate "${4:-0}" -reorder "${5:-0}" \
        -delay "${6:-0}" -jitter "${7:-0}" >/dev/null 2>&1 &
}

test_basic() {
    nm="$1" sz="$2" gen="$3" srvto="$4" port="$5"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    srv "$port" "$out" "$srvto" &
    spid=$!
    sleep 0.2
    cli "$port" "$in" "$srvto"
    wait $spid
    cmp -s "$in" "$out"
}

test_proxied() {
    nm="$1" sz="$2" gen="$3" srvto="$4" pp="$5" tp="$6" l="$7" d="$8" r="$9" dl="${10}" ji="${11}"
    in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    proxy "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
    xpid=$!
    sleep 0.4
    srv "$tp" "$out" "$srvto" &
    spid=$!
    sleep 0.2
    cli "$pp" "$in" "$srvto"
    wait $spid
    ok=$?
    kill $xpid 2>/dev/null
    wait $xpid 2>/dev/null
    [ $ok -eq 0 ] && cmp -s "$in" "$out"
}

# ---------------------------------------------------------
# 1) BASIC TRANSFERS
# ---------------------------------------------------------
header "BASIC"

echo "Short_text_500B 500 mktxt 5 10001
Empty_input 0 mktxt 5 10002
1KB_zeros 1024 mkzero 5 10003
100KB_text 102400 mktxt 15 10004
1MB_random 1048576 mkrand 30 10005
All_bytes_0-255 256 mkall 5 10007" | while read -r nm sz gen to port; do
    test_one "$nm" test_basic "$nm" "$sz" "$gen" "$to" "$port"
done

test_one "Stdin->stdout" sh -c '
    base='"$TMPDIR"'
    echo "Hello stdin test 12345" > "$base/std.in"
    ./ipk-rdt -s -p 10006 -o - -w 5 > "$base/std.out" 2>/dev/null &
    spid=$!
    sleep 0.2
    cat "$base/std.in" | ./ipk-rdt -c -a 127.0.0.1 -p 10006 -i - -w 5 2>/dev/null
    wait $spid
    cmp -s "$base/std.in" "$base/std.out"
'

# ---------------------------------------------------------
# 2) IMPAIRMENT TESTS
# ---------------------------------------------------------
header "LOSS / DUP / REORDER / DELAY"

echo "10%_loss_50KB 51200 20 10008 20001 10 0 0 0 0
20%_loss_30KB 30720 20 10009 20002 20 0 0 0 0
30%_loss_20KB 20480 20 10010 20003 30 0 0 0 0
20%_dup_20KB 20480 20 10011 20004 0 20 0 0 0
40%_reorder_20KB 20480 20 10012 20005 0 0 40 0 0
50ms_delay_15ms_jitter_15KB 15360 25 10013 20006 0 0 0 50 15
Combo_moderate_15KB 15360 30 10014 20007 10 10 20 30 10
Combo_extreme_10KB 10240 30 10015 20008 20 0 30 40 20" | while read -r nm sz srvto pp tp l d r dl ji; do
    test_one "$nm" test_proxied "$nm" "$sz" mktxt "$srvto" "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
done

# ---------------------------------------------------------
# 3) TIMEOUT
# ---------------------------------------------------------
header "TIMEOUT"

test_one "Client_timeout_no_server" sh -c '
    ./ipk-rdt -c -a 127.0.0.1 -p 55555 -i /dev/null -w 2 >/dev/null 2>&1
    [ $? -ne 0 ]
'

test_one "Empty_input_teardown" sh -c '
    base='"$TMPDIR"'
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
    base='"$TMPDIR"'
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
# ============================================================
# IPK-RDT spolehlivý test runner
# Kopíruj do VM a spusť: bash test_runner.sh
# ============================================================
set -euo pipefail

TMPDIR=$(mktemp -d /tmp/ipk.XXXX)
trap "rm -rf $TMPDIR" EXIT

# barvy
G='\033[32m'; R='\033[31m'; C='\033[36m'; N='\033[0m'

pass=0 fail=0 total=0

header(){ echo -e "\n${C}=== $1 ===${N}"; }

test_one(){
    local name="$1"; shift
    total=$((total+1))
    printf "  [%2d] %-50s ... " $total "$name"
    local start=$(date +%s)
    local out="$TMPDIR/out"
    if "$@" >"$out" 2>&1; then
        local t=$(($(date +%s)-start))
        echo -e "${G}PASS${N} (${t}s)"
        pass=$((pass+1))
    else
        local t=$(($(date +%s)-start))
        echo -e "${R}FAIL${N} (${t}s)"
        tail -3 "$out" | while read l; do echo "        | $l"; done
        fail=$((fail+1))
    fi
}

# ---------------------------------------------------------
header "BUILD"
go build -o ipk-rdt . 2>&1
echo "  Binary ready."

# ---------------------------------------------------------
# Pomocné funkce – normální shell, žádné subshelly  
# ---------------------------------------------------------

mktxt(){
    # opakovaný blok do souboru přesné velikosti
    local sz=$1 f=$2
    local blk="Hello IPK reliable test. 0123456789 ABCDEF. The quick brown fox. "
    local bl=${#blk}
    :>"$f"
    local w=0
    while [ $w -lt $sz ]; do
        local r=$((sz-w))
        if [ $r -ge $bl ]; then printf "%s" "$blk" >>"$f"; w=$((w+bl))
        else printf "%.${r}s" "$blk" >>"$f"; w=$sz
        fi
    done
}

mkzero(){ dd if=/dev/zero of="$2" bs="$1" count=1 2>/dev/null; }
mkrand(){ dd if=/dev/urandom of="$2" bs="$1" count=1 2>/dev/null; }
mkall(){
    :>"$1"
    for i in $(seq 0 255); do printf "\\$(printf '%03o' $i)" >>"$1"; done
}

srv(){
    # server: port outfile timeout
    ./ipk-rdt -s -p "$1" -a 127.0.0.1 -o "$2" -w "$3" >/dev/null 2>&1
}

cli(){
    # client: port infile timeout
    ./ipk-rdt -c -a 127.0.0.1 -p "$1" -i "$2" -w "$3" >/dev/null 2>&1
}

proxy(){
    go run test_proxy.go \
        -listen "127.0.0.1:$1" \
        -target "127.0.0.1:$2" \
        -loss "${3:-0}" -duplicate "${4:-0}" -reorder "${5:-0}" \
        -delay "${6:-0}" -jitter "${7:-0}" >/dev/null 2>&1
}

test_basic(){
    local nm=$1 sz=$2 gen=$3 srvto=$4 port=$5
    local in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    srv "$port" "$out" "$srvto" &
    local spid=$!
    sleep 0.2
    cli "$port" "$in" "$srvto"
    wait $spid
    cmp -s "$in" "$out"
}

test_proxied(){
    local nm=$1 sz=$2 gen=$3 srvto=$4 pp=$5 tp=$6 l=$7 d=$8 r=$9 dl=${10} ji=${11}
    local in="$TMPDIR/$nm.in" out="$TMPDIR/$nm.out"
    $gen "$sz" "$in"
    proxy "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji" &
    local xpid=$!
    sleep 0.4
    srv "$tp" "$out" "$srvto" &
    local spid=$!
    sleep 0.2
    cli "$pp" "$in" "$srvto"
    wait $spid; local ok=$?
    kill $xpid 2>/dev/null; wait $xpid 2>/dev/null
    [ $ok -eq 0 ] && cmp -s "$in" "$out"
}

# ---------------------------------------------------------
# 1) BASIC TRANSFERS
# ---------------------------------------------------------
header "BASIC"

for t in \
    "Short_text_500B|500|mktxt|5|10001" \
    "Empty_input|0|mktxt|5|10002" \
    "1KB_zeros|1024|mkzero|5|10003" \
    "100KB_text|102400|mktxt|15|10004" \
    "1MB_random|1048576|mkrand|30|10005" \
    "All_bytes_0-255|256|mkall|5|10007"
do
    IFS='|' read nm sz gen to port <<<"$t"
    test_one "$nm" test_basic "$nm" "$sz" "$gen" "$to" "$port"
done

# stdin/stdout zvlášť
test_one "Stdin->stdout" bash -c "
    base=$TMPDIR/std
    echo 'Hello stdin test 12345' > \$base.in
    ./ipk-rdt -s -p 10006 -o - -w 5 > \$base.out 2>/dev/null &
    spid=\$!
    sleep 0.2
    cat \$base.in | ./ipk-rdt -c -a 127.0.0.1 -p 10006 -i - -w 5 2>/dev/null
    wait \$spid
    cmp -s \$base.in \$base.out
"

# ---------------------------------------------------------
# 2) IMPAIRMENT TESTS
# ---------------------------------------------------------
header "LOSS / DUP / REORDER / DELAY"

for t in \
    "10%_loss_50KB|51200|20|10008|20001|10|0|0|0|0" \
    "20%_loss_30KB|30720|20|10009|20002|20|0|0|0|0" \
    "30%_loss_20KB|20480|20|10010|20003|30|0|0|0|0" \
    "20%_dup_20KB|20480|20|10011|20004|0|20|0|0|0" \
    "40%_reorder_20KB|20480|20|10012|20005|0|0|40|0|0" \
    "50ms_delay_15ms_jitter_15KB|15360|25|10013|20006|0|0|0|50|15" \
    "Combo_moderate_15KB|15360|30|10014|20007|10|10|20|30|10" \
    "Combo_extreme_10KB|10240|30|10015|20008|20|0|30|40|20"
do
    IFS='|' read nm sz to pp tp l d r dl ji <<<"$t"
    test_one "$nm" test_proxied "$nm" "$sz" mktxt "$to" "$pp" "$tp" "$l" "$d" "$r" "$dl" "$ji"
done

# ---------------------------------------------------------
# 3) TIMEOUT
# ---------------------------------------------------------
header "TIMEOUT"

test_one "Client_timeout_no_server" bash -c "
    ./ipk-rdt -c -a 127.0.0.1 -p 55555 -i /dev/null -w 2 >/dev/null 2>&1; [ \$? -ne 0 ]
"

test_one "Empty_input_teardown" bash -c "
    base=$TMPDIR/et
    touch \$base.in
    ./ipk-rdt -s -p 10016 -o \$base.out -w 5 >/dev/null 2>&1 &
    spid=\$!
    sleep 0.2
    ./ipk-rdt -c -a 127.0.0.1 -p 10016 -i \$base.in -w 5 >/dev/null 2>&1
    wait \$spid
    cmp -s \$base.in \$base.out
"

# ---------------------------------------------------------
# 4) WINDOW / PIPELINING
# ---------------------------------------------------------
header "WINDOW"

test_one "200KB_random_window" bash -c "
    base=$TMPDIR/win
    dd if=/dev/urandom of=\$base.in bs=204800 count=1 2>/dev/null
    ./ipk-rdt -s -p 10017 -o \$base.out -w 40 >/dev/null 2>&1 &
    spid=\$!
    sleep 0.2
    ./ipk-rdt -c -a 127.0.0.1 -p 10017 -i \$base.in -w 40 >/dev/null 2>&1
    wait \$spid
    cmp -s \$base.in \$base.out
"

# ---------------------------------------------------------
# RESULTS
# ---------------------------------------------------------
header "RESULTS"
echo ""
echo "  Total:  $total"
echo -e "  ${G}Passed: $pass${N}"
echo -e "  ${R}Failed: $fail${N}"
echo ""
[ $fail -eq 0 ] && echo -e "  ${G}ALL TESTS PASSED${N}" || echo -e "  ${C}Some failed${N}"

exit $fail
package ports

import (
	"encoding/binary"
	"strconv"
	"testing"
	"testing/fstest"
	"time"
)

// auxv encodes key/value pairs the way the kernel writes them: native-endian
// words, terminated by AT_NULL.
func auxv(pairs ...uint64) []byte {
	pairs = append(pairs, 0, 0)
	var b []byte
	for _, v := range pairs {
		if strconv.IntSize == 32 {
			b = binary.NativeEndian.AppendUint32(b, uint32(v))
		} else {
			b = binary.NativeEndian.AppendUint64(b, v)
		}
	}
	return b
}

const procStat = "cpu  1 2 3 4\nbtime 1700000000\nprocesses 42\n"

// pidStat is a <pid>/stat line whose command name contains ") (" and spaces,
// with starttime (field 22) set to 12345 ticks.
const pidStat = "42 (my ) (app) S 1 42 42 0 -1 4194560 100 0 0 0 5 3 0 0 20 0 1 0 12345 1000000 200 18446744073709551615\n"

func TestClockStarted(t *testing.T) {
	procfs := fstest.MapFS{
		"stat":      {Data: []byte(procStat)},
		"self/auxv": {Data: auxv(6, 4096, atClkTck, 100, 25, 0)},
		"42/stat":   {Data: []byte(pidStat)},
	}
	clk, err := readClock(procfs)
	if err != nil {
		t.Fatal(err)
	}
	if clk.hz != 100 {
		t.Errorf("hz = %d, want 100", clk.hz)
	}
	want := time.Unix(1700000000, 0).Add(123*time.Second + 450*time.Millisecond)
	if got := clk.started(procfs, 42); !got.Equal(want) {
		t.Errorf("started = %v, want %v", got, want)
	}
	if got := clk.started(procfs, 99); !got.IsZero() {
		t.Errorf("started for missing pid = %v, want zero", got)
	}
}

func TestReadClockErrors(t *testing.T) {
	tests := map[string]fstest.MapFS{
		"no stat":   {"self/auxv": {Data: auxv(atClkTck, 100)}},
		"no btime":  {"stat": {Data: []byte("cpu 1\n")}, "self/auxv": {Data: auxv(atClkTck, 100)}},
		"no auxv":   {"stat": {Data: []byte(procStat)}},
		"no clktck": {"stat": {Data: []byte(procStat)}, "self/auxv": {Data: auxv(6, 4096)}},
	}
	for name, procfs := range tests {
		if _, err := readClock(procfs); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

package ports

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"
)

// atClkTck is the auxiliary vector key for the clock tick rate (USER_HZ).
const atClkTck = 17

// startTimeField is the position of starttime in <pid>/stat, counted from
// the first field after the parenthesized command name (proc(5) field 22).
const startTimeField = 22 - 3

// clock converts process start times, which the kernel reports in clock
// ticks since boot, into wall-clock times.
type clock struct {
	boot time.Time
	hz   uint64
}

func readClock(procfs fs.FS) (clock, error) {
	boot, err := bootTime(procfs)
	if err != nil {
		return clock{}, err
	}
	hz, err := clockTicks(procfs)
	if err != nil {
		return clock{}, err
	}
	return clock{boot: boot, hz: hz}, nil
}

// bootTime reads the "btime" line of stat.
func bootTime(procfs fs.FS) (time.Time, error) {
	b, err := fs.ReadFile(procfs, "stat")
	if err != nil {
		return time.Time{}, err
	}
	for line := range strings.Lines(string(b)) {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, errors.New("stat: bad btime")
			}
			return time.Unix(sec, 0), nil
		}
	}
	return time.Time{}, errors.New("stat: no btime line")
}

// clockTicks reads the tick rate from this process's auxiliary vector, the
// same value sysconf(_SC_CLK_TCK) returns.
func clockTicks(procfs fs.FS) (uint64, error) {
	b, err := fs.ReadFile(procfs, "self/auxv")
	if err != nil {
		return 0, err
	}
	word := strconv.IntSize / 8
	for i := 0; i+2*word <= len(b); i += 2 * word {
		key, val := nativeWord(b[i:], word), nativeWord(b[i+word:], word)
		if key == atClkTck && val > 0 {
			return val, nil
		}
	}
	return 0, errors.New("auxv: no AT_CLKTCK entry")
}

func nativeWord(b []byte, size int) uint64 {
	if size == 4 {
		return uint64(binary.NativeEndian.Uint32(b))
	}
	return binary.NativeEndian.Uint64(b)
}

// started returns when pid started, or the zero time if it is unknown.
func (c clock) started(procfs fs.FS, pid int) time.Time {
	b, err := fs.ReadFile(procfs, path.Join(strconv.Itoa(pid), "stat"))
	if err != nil {
		return time.Time{}
	}
	// The command name may contain spaces and ')', so split after the last ')'.
	i := bytes.LastIndexByte(b, ')')
	if i < 0 {
		return time.Time{}
	}
	fields := strings.Fields(string(b[i+1:]))
	if len(fields) <= startTimeField {
		return time.Time{}
	}
	ticks, err := strconv.ParseUint(fields[startTimeField], 10, 64)
	if err != nil {
		return time.Time{}
	}
	secs, rem := ticks/c.hz, ticks%c.hz
	return c.boot.Add(time.Duration(secs)*time.Second + time.Duration(rem)*time.Second/time.Duration(c.hz))
}

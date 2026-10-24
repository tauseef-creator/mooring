package ports

import (
	"io/fs"
	"path"
	"strconv"
	"strings"
)

// socketOwners maps socket inodes to the PID holding them open. Processes
// that cannot be inspected (other users', unless running as root) or that
// exit during the scan are skipped.
func socketOwners(procfs fs.FS) (map[uint64]int, error) {
	entries, err := fs.ReadDir(procfs, ".")
	if err != nil {
		return nil, err
	}
	owners := make(map[uint64]int)
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() {
			continue
		}
		fdDir := path.Join(e.Name(), "fd")
		fds, err := fs.ReadDir(procfs, fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := fs.ReadLink(procfs, path.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			inode, ok := socketInode(target)
			if !ok {
				continue
			}
			// Forked workers share their parent's socket; report the lowest PID.
			if cur, seen := owners[inode]; !seen || pid < cur {
				owners[inode] = pid
			}
		}
	}
	return owners, nil
}

// socketInode extracts 123 from a "socket:[123]" fd link target.
func socketInode(target string) (uint64, bool) {
	s, ok := strings.CutPrefix(target, "socket:[")
	if !ok {
		return 0, false
	}
	s, ok = strings.CutSuffix(s, "]")
	if !ok {
		return 0, false
	}
	inode, err := strconv.ParseUint(s, 10, 64)
	return inode, err == nil
}

// command returns a process's command line with the program's directory
// trimmed, such as "python3 -m http.server". It falls back to the short name
// in comm for kernel threads and processes that cleared their arguments.
func command(procfs fs.FS, pid int) string {
	dir := strconv.Itoa(pid)
	if b, err := fs.ReadFile(procfs, path.Join(dir, "cmdline")); err == nil {
		args := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
		if args[0] != "" {
			// Some programs rewrite argv[0] into a title like
			// "nginx: master process"; only trim real paths.
			if !strings.Contains(args[0], " ") {
				args[0] = path.Base(args[0])
			}
			return strings.Join(args, " ")
		}
	}
	b, _ := fs.ReadFile(procfs, path.Join(dir, "comm"))
	return strings.TrimSpace(string(b))
}

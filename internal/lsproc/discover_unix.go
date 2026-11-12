//go:build !windows

package lsproc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

func standaloneProcesses() ([]process, error) {
	if procs, err := standaloneProcessesViaProc(); err == nil && len(procs) > 0 {
		return procs, nil
	}
	ctx, cancel := discoveryContext(context.Background())
	defer cancel()
	return standaloneProcessesViaPS(ctx)
}

func standaloneProcessesViaProc() ([]process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	var procs []process
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}

		cmdlineBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			continue
		}

		args := strings.Split(string(cmdlineBytes), "\x00")
		cmdline := strings.Join(args, " ")
		cmdline = strings.TrimSpace(cmdline)

		if cmdline == "" || !isStandaloneLS(cmdline) {
			continue
		}

		procs = append(procs, process{pid: pid, cmdline: cmdline})
	}

	if len(procs) == 0 {
		return nil, errors.New("no standalone processes found in /proc")
	}
	return procs, nil
}

func standaloneProcessesViaPS(ctx context.Context) ([]process, error) {
	out, err := exec.CommandContext(ctx, "ps", "ax", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}

	var procs []process
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !isStandaloneLS(line) {
			continue
		}

		fields := strings.SplitN(line, " ", 2)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		procs = append(procs, process{pid: pid, cmdline: fields[1]})
	}
	return procs, nil
}

func listeningPorts(pid int) ([]int, error) {
	ctx, cancel := discoveryContext(context.Background())
	defer cancel()
	return listeningPortsContext(ctx, pid)
}

func listeningPortsContext(ctx context.Context, pid int) ([]int, error) {
	ports, _, err := discoverPIDsListeners(ctx, []int{pid})
	if err != nil {
		return nil, err
	}
	return ports, nil
}

func processTreeListeningEndpoints(ctx context.Context, rootPID int) (map[int]LoopbackListenMask, error) {
	pids := descendantPIDs(ctx, rootPID)
	_, endpoints, err := discoverPIDsListeners(ctx, pids)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, errors.New("no loopback listening endpoints found for process tree")
	}
	return endpoints, nil
}

func discoverPIDsListeners(ctx context.Context, pids []int) ([]int, map[int]LoopbackListenMask, error) {
	if len(pids) == 0 {
		return nil, nil, errors.New("no pids to inspect")
	}
	if ports, endpoints, err := listenersForPIDsViaProc(pids); err == nil && len(ports) > 0 {
		return ports, endpoints, nil
	}
	if ports, endpoints, err := listenersForPIDsViaLsof(ctx, pids); err == nil && len(ports) > 0 {
		return ports, endpoints, nil
	}
	if ports, endpoints, err := listenersForPIDsViaSS(ctx, pids); err == nil && len(ports) > 0 {
		return ports, endpoints, nil
	}
	return nil, nil, errors.New("could not determine listening ports")
}

func portsViaProc(pid int) ([]int, error) {
	ports, _, err := listenersForPIDsViaProc([]int{pid})
	return ports, err
}

func listenersForPIDsViaProc(pids []int) ([]int, map[int]LoopbackListenMask, error) {
	socketInodes := make(map[string]bool)
	for _, pid := range pids {
		fdDir := fmt.Sprintf("/proc/%d/fd", pid)
		entries, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
			if err != nil {
				continue
			}
			if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
				inode := target[8 : len(target)-1]
				socketInodes[inode] = true
			}
		}
	}

	if len(socketInodes) == 0 {
		return nil, nil, errors.New("no socket inodes found in /proc")
	}

	var ports []int
	seen := make(map[int]bool)
	endpoints := make(map[int]LoopbackListenMask)

	for _, src := range []struct {
		path string
		ipv6 bool
	}{
		{path: "/proc/net/tcp", ipv6: false},
		{path: "/proc/net/tcp6", ipv6: true},
	} {
		data, err := os.ReadFile(src.path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			if fields[3] != "0A" || !socketInodes[fields[9]] { // 0A = TCP_LISTEN
				continue
			}
			addrParts := strings.Split(fields[1], ":")
			if len(addrParts) != 2 {
				continue
			}
			port64, err := strconv.ParseInt(addrParts[1], 16, 32)
			if err != nil {
				continue
			}
			port := int(port64)
			if port <= 0 || port > 65535 {
				continue
			}
			if !seen[port] {
				seen[port] = true
				ports = append(ports, port)
			}
			if mask := parseProcHexAddrMask(addrParts[0], src.ipv6); mask != 0 {
				endpoints[port] |= mask
			}
		}
	}

	if len(ports) == 0 {
		return nil, nil, errors.New("no listening ports matched socket inodes in /proc")
	}
	return ports, endpoints, nil
}

func parseProcHexAddrMask(hexAddr string, ipv6 bool) LoopbackListenMask {
	if !ipv6 {
		if len(hexAddr) != 8 {
			return 0
		}
		v, err := strconv.ParseUint(hexAddr, 16, 32)
		if err != nil {
			return 0
		}
		var b [4]byte
		binary.NativeEndian.PutUint32(b[:], uint32(v))
		ip := net.IPv4(b[0], b[1], b[2], b[3])
		if ip.IsUnspecified() || ip.IsLoopback() {
			return ListenIPv4
		}
		return 0
	}

	if len(hexAddr) != 32 {
		return 0
	}
	var b [16]byte
	for i := 0; i < 4; i++ {
		w, err := strconv.ParseUint(hexAddr[i*8:(i+1)*8], 16, 32)
		if err != nil {
			return 0
		}
		binary.NativeEndian.PutUint32(b[i*4:(i+1)*4], uint32(w))
	}
	ip := net.IP(b[:])
	if ip4 := ip.To4(); ip4 != nil {
		if ip4.IsUnspecified() || ip4.IsLoopback() {
			return ListenIPv4
		}
		return 0
	}
	if ip.IsUnspecified() {
		return ListenIPv6Wildcard
	}
	if ip.IsLoopback() {
		return ListenIPv6
	}
	return 0
}

func descendantPIDs(ctx context.Context, rootPID int) []int {
	children := childPIDMapViaProc()
	if len(children) == 0 {
		children = childPIDMapViaPS(ctx)
	}

	seen := map[int]bool{rootPID: true}
	queue := []int{rootPID}
	for idx := 0; idx < len(queue); idx++ {
		cur := queue[idx]
		for _, child := range children[cur] {
			if child > 0 && !seen[child] {
				seen[child] = true
				queue = append(queue, child)
			}
		}
	}
	return queue
}

type procStatMeta struct {
	ppid      int
	startTime uint64
}

func childPIDMapViaProc() map[int][]int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	meta := make(map[int]procStatMeta, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			continue
		}
		closeIdx := strings.LastIndexByte(string(data), ')')
		if closeIdx < 0 {
			continue
		}
		fields := strings.Fields(string(data[closeIdx+1:]))
		if len(fields) < 2 || fields[0] == "Z" {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil || ppid <= 0 {
			continue
		}
		var startTicks uint64
		if len(fields) > 19 {
			startTicks, _ = strconv.ParseUint(fields[19], 10, 64)
		}
		meta[pid] = procStatMeta{ppid: ppid, startTime: startTicks}
	}

	children := make(map[int][]int)
	for pid, m := range meta {
		if parent, ok := meta[m.ppid]; ok && parent.startTime > 0 && m.startTime > 0 && m.startTime < parent.startTime {
			// Guard against PID reuse where a stale parent PID now belongs to a newer process.
			continue
		}
		children[m.ppid] = append(children[m.ppid], pid)
	}
	return children
}

func childPIDMapViaPS(ctx context.Context) map[int][]int {
	out, err := exec.CommandContext(ctx, "ps", "ax", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	children := make(map[int][]int)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil || pid <= 0 || ppid <= 0 {
			continue
		}
		children[ppid] = append(children[ppid], pid)
	}
	return children
}

var lsofListenLineRe = regexp.MustCompile(`\bTCP\s+(\S+):(\d+)\s+\(LISTEN\)`)

func listenersForPIDsViaLsof(ctx context.Context, pids []int) ([]int, map[int]LoopbackListenMask, error) {
	const batchSize = 256
	var allPorts []int
	seen := make(map[int]bool)
	endpoints := make(map[int]LoopbackListenMask)

	for start := 0; start < len(pids); start += batchSize {
		end := start + batchSize
		if end > len(pids) {
			end = len(pids)
		}
		parts := make([]string, 0, end-start)
		for _, pid := range pids[start:end] {
			if pid > 0 {
				parts = append(parts, strconv.Itoa(pid))
			}
		}
		if len(parts) == 0 {
			continue
		}
		out, err := exec.CommandContext(ctx, "lsof", "-a", "-p", strings.Join(parts, ","), "-iTCP", "-sTCP:LISTEN", "-P", "-n").Output()
		if len(out) == 0 && err != nil {
			continue
		}
		parseLsofOutput(string(out), &allPorts, seen, endpoints)
	}

	if len(allPorts) == 0 {
		return nil, nil, errors.New("no listening ports found via lsof")
	}
	return allPorts, endpoints, nil
}

func parseLsofOutput(out string, ports *[]int, seen map[int]bool, endpoints map[int]LoopbackListenMask) {
	for _, line := range strings.Split(out, "\n") {
		m := lsofListenLineRe.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		port, err := strconv.Atoi(m[2])
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		if !seen[port] {
			seen[port] = true
			*ports = append(*ports, port)
		}
		ipv6Hint := strings.Contains(line, "IPv6") || strings.HasPrefix(m[1], "[")
		if mask := parseListenHostMask(m[1], ipv6Hint); mask != 0 {
			endpoints[port] |= mask
		}
	}
}

var ssPIDRe = regexp.MustCompile(`pid=(\d+)\b`)

func listenersForPIDsViaSS(ctx context.Context, pids []int) ([]int, map[int]LoopbackListenMask, error) {
	out, err := exec.CommandContext(ctx, "ss", "-tlnpH").Output()
	if err != nil {
		return nil, nil, err
	}
	pidSet := make(map[int]bool, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			pidSet[pid] = true
		}
	}

	var ports []int
	seen := make(map[int]bool)
	endpoints := make(map[int]LoopbackListenMask)
	parseSSOutput(string(out), pidSet, &ports, seen, endpoints)
	if len(ports) == 0 {
		return nil, nil, errors.New("no listening ports found via ss")
	}
	return ports, endpoints, nil
}

func parseSSOutput(out string, pidSet map[int]bool, ports *[]int, seen map[int]bool, endpoints map[int]LoopbackListenMask) {
	for _, line := range strings.Split(out, "\n") {
		matchedPID := false
		for _, pm := range ssPIDRe.FindAllStringSubmatch(line, -1) {
			if pid, err := strconv.Atoi(pm[1]); err == nil && pidSet[pid] {
				matchedPID = true
				break
			}
		}
		if !matchedPID {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		localAddr := fields[3]
		colon := strings.LastIndexByte(localAddr, ':')
		if colon <= 0 || colon+1 >= len(localAddr) {
			continue
		}
		hostPart := localAddr[:colon]
		port, err := strconv.Atoi(localAddr[colon+1:])
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		if !seen[port] {
			seen[port] = true
			*ports = append(*ports, port)
		}
		ipv6Hint := strings.HasPrefix(hostPart, "[") || strings.Contains(hostPart, ":")
		if mask := parseListenHostMask(hostPart, ipv6Hint); mask != 0 {
			endpoints[port] |= mask
		}
	}
}

func sendTerminate(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if idx := strings.LastIndexByte(string(data), ')'); idx >= 0 {
			rest := strings.TrimSpace(string(data[idx+1:]))
			if strings.HasPrefix(rest, "Z") {
				return false
			}
		}
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func canControlProcess(pid int) bool {
	return isProcessAlive(pid) && syscall.Kill(pid, 0) == nil
}

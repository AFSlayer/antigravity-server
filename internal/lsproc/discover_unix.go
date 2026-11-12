//go:build !windows

package lsproc

import (
	"errors"
	"fmt"
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
	return standaloneProcessesViaPS()
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

func standaloneProcessesViaPS() ([]process, error) {
	out, err := exec.Command("ps", "ax", "-o", "pid=,command=").Output()
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
	if ports, err := portsViaProc(pid); err == nil && len(ports) > 0 {
		return ports, nil
	}
	if ports, err := portsViaLsof(pid); err == nil && len(ports) > 0 {
		return ports, nil
	}
	if ports, err := portsViaSS(pid); err == nil && len(ports) > 0 {
		return ports, nil
	}
	return nil, errors.New("could not determine listening ports")
}

func portsViaProc(pid int) ([]int, error) {
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		return nil, err
	}

	socketInodes := make(map[string]bool)
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

	if len(socketInodes) == 0 {
		return nil, errors.New("no socket inodes found in /proc")
	}

	var ports []int
	seen := make(map[int]bool)
	for _, netFile := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		data, err := os.ReadFile(netFile)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 {
				continue
			}
			state := fields[3]
			inode := fields[9]
			if state == "0A" && socketInodes[inode] { // 0A = TCP_LISTEN
				addrParts := strings.Split(fields[1], ":")
				if len(addrParts) == 2 {
					if port64, err := strconv.ParseInt(addrParts[1], 16, 32); err == nil {
						port := int(port64)
						if port > 0 && port <= 65535 && !seen[port] {
							seen[port] = true
							ports = append(ports, port)
						}
					}
				}
			}
		}
	}

	if len(ports) == 0 {
		return nil, errors.New("no listening ports matched socket inodes in /proc")
	}
	return ports, nil
}

var lsofPortRe = regexp.MustCompile(`:(\d+)\s+\(LISTEN\)`)

func portsViaLsof(pid int) ([]int, error) {
	out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(pid), "-iTCP", "-sTCP:LISTEN", "-P", "-n").Output()
	if err != nil {
		return nil, err
	}
	return collectPorts(string(out), lsofPortRe, ""), nil
}

var ssPortRe = regexp.MustCompile(`[:\]](\d+)\s`)

func portsViaSS(pid int) ([]int, error) {
	out, err := exec.Command("ss", "-tlnpH").Output()
	if err != nil {
		return nil, err
	}
	return collectPorts(string(out), ssPortRe, "pid="+strconv.Itoa(pid)+","), nil
}

func collectPorts(out string, re *regexp.Regexp, mustContain string) []int {
	var ports []int
	seen := map[int]bool{}

	for _, line := range strings.Split(out, "\n") {
		if mustContain != "" && !strings.Contains(line, mustContain) {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			port, err := strconv.Atoi(m[1])
			if err != nil || port <= 0 || port > 65535 || seen[port] {
				continue
			}
			seen[port] = true
			ports = append(ports, port)
		}
	}
	return ports
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

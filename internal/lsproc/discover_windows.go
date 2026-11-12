//go:build windows

package lsproc

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func powershell(script string) (string, error) {
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return string(out), err
}

const listProcessesScript = `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*language_server*' -and $_.CommandLine -like '*--standalone*' } | ForEach-Object { "$($_.ProcessId)|$($_.CommandLine)" }`

func standaloneProcesses() ([]process, error) {
	out, err := powershell(listProcessesScript)
	if err != nil {
		return nil, err
	}

	var procs []process
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(parts[0])
		if err != nil || !isStandaloneLS(parts[1]) {
			continue
		}
		procs = append(procs, process{pid: pid, cmdline: parts[1]})
	}
	return procs, nil
}

func listeningPorts(pid int) ([]int, error) {
	script := `Get-NetTCPConnection -State Listen -OwningProcess ` + strconv.Itoa(pid) +
		` -ErrorAction SilentlyContinue | ForEach-Object { $_.LocalPort }`

	out, err := powershell(script)
	if err != nil {
		return nil, err
	}

	var ports []int
	seen := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		port, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	return ports, nil
}

func processTreeListeningPorts(rootPID int) ([]int, error) {
	script := `$children = @{}; ` +
		`Get-CimInstance Win32_Process | ForEach-Object { ` +
		`  $ppid = [int]$_.ParentProcessId; $pidVal = [int]$_.ProcessId; ` +
		`  if (-not $children.ContainsKey($ppid)) { $children[$ppid] = @() }; ` +
		`  $children[$ppid] += $pidVal ` +
		`}; ` +
		`$seen = @{}; $queue = New-Object System.Collections.Queue; ` +
		`$seen[` + strconv.Itoa(rootPID) + `] = $true; $queue.Enqueue(` + strconv.Itoa(rootPID) + `); ` +
		`while ($queue.Count -gt 0) { ` +
		`  $cur = $queue.Dequeue(); ` +
		`  if ($children.ContainsKey($cur)) { ` +
		`    foreach ($c in $children[$cur]) { if (-not $seen.ContainsKey($c)) { $seen[$c] = $true; $queue.Enqueue($c) } } ` +
		`  } ` +
		`}; ` +
		`Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $seen.ContainsKey([int]$_.OwningProcess) } | ForEach-Object { $_.LocalPort }`

	out, err := powershell(script)
	if err != nil {
		return nil, err
	}

	var ports []int
	seen := map[int]bool{}
	for _, line := range strings.Split(out, "\n") {
		port, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || port <= 0 || port > 65535 || seen[port] {
			continue
		}
		seen[port] = true
		ports = append(ports, port)
	}
	if len(ports) == 0 {
		return nil, errors.New("could not determine listening ports for process tree")
	}
	return ports, nil
}

func sendTerminate(proc *os.Process) error {
	return proc.Kill()
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(h)
	s, err := syscall.WaitForSingleObject(h, 0)
	return err == nil && s == syscall.WAIT_TIMEOUT
}

func canControlProcess(pid int) bool {
	if !isProcessAlive(pid) {
		return false
	}
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

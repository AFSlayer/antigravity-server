//go:build windows

package lsproc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func powershell(ctx context.Context, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	return string(out), err
}

const listProcessesScript = `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*language_server*' -and $_.CommandLine -like '*--standalone*' } | ForEach-Object { "$($_.ProcessId)|$($_.CommandLine)" }`

func standaloneProcesses() ([]process, error) {
	ctx, cancel := discoveryContext(context.Background())
	defer cancel()
	out, err := powershell(ctx, listProcessesScript)
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
	ctx, cancel := discoveryContext(context.Background())
	defer cancel()
	return listeningPortsContext(ctx, pid)
}

func listeningPortsContext(ctx context.Context, pid int) ([]int, error) {
	script := `Get-NetTCPConnection -State Listen -OwningProcess ` + strconv.Itoa(pid) +
		` -ErrorAction SilentlyContinue | ForEach-Object { $_.LocalPort }`

	out, err := powershell(ctx, script)
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
	return ports, nil
}

func processTreeListeners(ctx context.Context, rootPID int) ([]int, map[int]LoopbackListenMask, error) {
	rootStr := strconv.Itoa(rootPID)
	script := `$children = @{}; $created = @{}; ` +
		`Get-CimInstance Win32_Process | ForEach-Object { ` +
		`  $ppid = [int]$_.ParentProcessId; $pidVal = [int]$_.ProcessId; ` +
		`  $ticks = [long]0; if ($_.CreationDate -is [datetime]) { $ticks = $_.CreationDate.Ticks }; ` +
		`  $created[$pidVal] = $ticks; ` +
		`  if (-not $children.ContainsKey($ppid)) { $children[$ppid] = @() }; ` +
		`  $children[$ppid] += $pidVal ` +
		`}; ` +
		`$seen = @{}; $queue = New-Object System.Collections.Queue; ` +
		`$seen[` + rootStr + `] = $true; $queue.Enqueue(` + rootStr + `); ` +
		`while ($queue.Count -gt 0) { ` +
		`  $cur = [int]$queue.Dequeue(); ` +
		`  $pTicks = if ($created.ContainsKey($cur)) { [long]$created[$cur] } else { [long]0 }; ` +
		`  if ($children.ContainsKey($cur)) { ` +
		`    foreach ($c in $children[$cur]) { ` +
		`      $cTicks = if ($created.ContainsKey($c)) { [long]$created[$c] } else { [long]0 }; ` +
		`      if ($pTicks -gt 0 -and $cTicks -gt 0 -and $cTicks -lt $pTicks) { continue }; ` +
		`      if (-not $seen.ContainsKey($c)) { $seen[$c] = $true; $queue.Enqueue($c) } ` +
		`    } ` +
		`  } ` +
		`}; ` +
		`Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue | Where-Object { $seen.ContainsKey([int]$_.OwningProcess) } | ForEach-Object { "$($_.LocalAddress)|$($_.LocalPort)" }`

	out, err := powershell(ctx, script)
	if err != nil {
		return nil, nil, err
	}

	var ports []int
	seen := map[int]bool{}
	endpoints := make(map[int]LoopbackListenMask)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 2)
		if len(parts) != 2 {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || port <= 0 || port > 65535 {
			continue
		}
		if !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
		ipv6Hint := strings.Contains(parts[0], ":")
		if mask := parseListenHostMask(parts[0], ipv6Hint); mask != 0 {
			endpoints[port] |= mask
		}
	}
	if len(ports) == 0 {
		return nil, nil, errors.New("could not determine listening ports for process tree")
	}
	return ports, endpoints, nil
}

func processTreeListeningPorts(ctx context.Context, rootPID int) ([]int, error) {
	ports, _, err := processTreeListeners(ctx, rootPID)
	return ports, err
}

func processTreeListeningEndpoints(ctx context.Context, rootPID int) (map[int]LoopbackListenMask, error) {
	_, endpoints, err := processTreeListeners(ctx, rootPID)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, errors.New("no loopback listening endpoints found for process tree")
	}
	return endpoints, nil
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

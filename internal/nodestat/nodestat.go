// Package nodestat reads host-level CPU and memory usage from /proc. In a
// standard container /proc/meminfo and /proc/stat are not namespaced, so they
// already reflect the host — the agent reads the real node usage without any
// extra mount or privilege. This is the node-wide signal the per-container
// Docker stats can't give (host processes, kernel, cache, non-Swarm workloads).
package nodestat

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Stats is a one-shot host usage sample.
type Stats struct {
	MemTotalBytes uint64
	MemUsedBytes  uint64 // total - available (like node-exporter; excludes reclaimable cache)
	CPUPercent    float64
	CPUCount      int
}

// Sampler reads host stats, keeping the previous CPU snapshot so each Sample
// reports utilisation over the interval since the last call.
type Sampler struct {
	root      string // /proc root, overridable in tests
	prevIdle  uint64
	prevTotal uint64
	havePrev  bool
}

// NewSampler returns a Sampler reading from /proc.
func NewSampler() *Sampler { return &Sampler{root: "/proc"} }

// Sample reads memory and CPU. Memory is instantaneous; CPU is the busy
// percentage since the previous Sample (0 on the first call, which has no
// baseline). A read error on one metric doesn't void the other.
func (s *Sampler) Sample() Stats {
	var st Stats
	if total, avail, ok := s.readMem(); ok {
		st.MemTotalBytes = total
		if total > avail {
			st.MemUsedBytes = total - avail
		}
	}
	if idle, total, count, ok := s.readCPU(); ok {
		st.CPUCount = count
		if s.havePrev && total > s.prevTotal {
			idleDelta := float64(idle - s.prevIdle)
			totalDelta := float64(total - s.prevTotal)
			if totalDelta > 0 {
				st.CPUPercent = clamp((1-idleDelta/totalDelta)*100, 0, 100)
			}
		}
		s.prevIdle, s.prevTotal, s.havePrev = idle, total, true
	}
	return st
}

// readMem parses MemTotal and MemAvailable (kB) from /proc/meminfo.
func (s *Sampler) readMem() (total, available uint64, ok bool) {
	f, err := os.Open(filepath.Join(s.root, "meminfo"))
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	var haveTotal, haveAvail bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				total, haveTotal = v*1024, true
			}
		case "MemAvailable:":
			if v, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				available, haveAvail = v*1024, true
			}
		}
		if haveTotal && haveAvail {
			break
		}
	}
	return total, available, haveTotal && haveAvail
}

// readCPU parses the aggregate "cpu" line of /proc/stat (jiffies) and counts the
// per-core "cpuN" lines. idle = idle + iowait; total = sum of all fields.
func (s *Sampler) readCPU() (idle, total uint64, count int, ok bool) {
	f, err := os.Open(filepath.Join(s.root, "stat"))
	if err != nil {
		return 0, 0, 0, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu") {
			break // the cpu lines are first; stop at the first non-cpu line
		}
		fields := strings.Fields(line)
		if fields[0] == "cpu" { // aggregate line
			for i, f := range fields[1:] {
				v, err := strconv.ParseUint(f, 10, 64)
				if err != nil {
					continue
				}
				total += v
				if i == 3 || i == 4 { // idle, iowait
					idle += v
				}
			}
			ok = true
		} else { // cpu0, cpu1, … per-core lines
			count++
		}
	}
	return idle, total, count, ok
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

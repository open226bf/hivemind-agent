package nodestat

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSampler(t *testing.T) {
	dir := t.TempDir()
	s := &Sampler{root: dir}

	write(t, filepath.Join(dir, "meminfo"),
		"MemTotal:       32000000 kB\nMemFree:         1000000 kB\nMemAvailable:    8000000 kB\n")
	// fields after "cpu": user nice system idle iowait irq softirq steal …
	write(t, filepath.Join(dir, "stat"),
		"cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 50 0 50 400 0 0 0 0 0 0\ncpu1 50 0 50 400 0 0 0 0 0 0\nintr 1\n")

	first := s.Sample()
	if first.MemTotalBytes != 32000000*1024 {
		t.Fatalf("mem total: got %d", first.MemTotalBytes)
	}
	if first.MemUsedBytes != (32000000-8000000)*1024 {
		t.Fatalf("mem used (total-available): got %d", first.MemUsedBytes)
	}
	if first.CPUCount != 2 {
		t.Fatalf("cpu count: got %d", first.CPUCount)
	}
	if first.CPUPercent != 0 {
		t.Fatalf("first CPU%% has no baseline, want 0, got %v", first.CPUPercent)
	}

	// Advance: idle +600 of total +800 → 25% busy.
	write(t, filepath.Join(dir, "stat"),
		"cpu  200 0 200 1400 0 0 0 0 0 0\ncpu0 100 0 100 700 0 0 0 0 0 0\ncpu1 100 0 100 700 0 0 0 0 0 0\nintr 2\n")
	second := s.Sample()
	if second.CPUPercent < 24.9 || second.CPUPercent > 25.1 {
		t.Fatalf("CPU%% over the interval: want ~25, got %v", second.CPUPercent)
	}
}

package changes

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
)

// BenchmarkCapture10000 提供 PERF-05 的固定真实目录入口；五次重复使用 -benchtime=5x。
// 每次基线采用全新 Chat 对象库，文件总量严格为 50 MiB，收尾只改动二十个文件。
func BenchmarkCapture10000(b *testing.B) {
	for _, repository := range []bool{false, true} {
		name := "shadow"
		if repository {
			name = "git"
		}
		b.Run(name, func(b *testing.B) {
			b.StopTimer()
			root := b.TempDir()
			const files = 10000
			const total = 50 << 20
			content := func(index int, changed bool) string {
				size := total / files
				if index < total%files {
					size++
				}
				prefix := fmt.Sprintf("file %05d changed=%v\n", index, changed)
				return prefix + strings.Repeat("x", size-len(prefix))
			}
			writeFile := func(index int, changed bool) {
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%05d.txt", index)), []byte(content(index, changed)), 0600); err != nil {
					b.Fatal(err)
				}
			}
			for i := 0; i < files; i++ {
				writeFile(i, false)
			}
			if repository {
				benchmarkGit(b, root, "init", "--quiet")
				benchmarkGit(b, root, "add", ".")
				benchmarkGit(b, root, "-c", "user.name=Snapshot Benchmark", "-c", "user.email=bench@example.invalid", "commit", "--quiet", "-m", "fixture")
			}
			baselineTimes, finishTimes := []float64{}, []float64{}
			for iteration := 0; iteration < b.N; iteration++ {
				for i := 0; i < 20; i++ {
					writeFile(i, false)
				}
				owned := b.TempDir()
				b.StartTimer()
				started := time.Now()
				capture, err := Begin(context.Background(), owned, fmt.Sprintf("benchmark-%d", iteration), []string{root}, Options{})
				baseline := time.Since(started)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				if capture.Summary().State != "pending" {
					b.Fatalf("baseline failed: %#v", capture.Summary())
				}
				beforeLogical, beforePhysical, beforeObjects := benchmarkStorage(b, owned)
				for i := 0; i < 20; i++ {
					writeFile(i, true)
				}
				b.StartTimer()
				started = time.Now()
				summary, err := capture.Finish(context.Background())
				finished := time.Since(started)
				b.StopTimer()
				if err != nil {
					b.Fatal(err)
				}
				if summary.State != "ready" || !summary.Complete || summary.TotalFiles != 20 {
					b.Fatalf("incomplete benchmark capture: %#v", summary)
				}
				afterLogical, afterPhysical, afterObjects := benchmarkStorage(b, owned)
				if afterObjects-beforeObjects != 21 {
					b.Fatalf("unchanged objects not deduplicated: before=%d after=%d", beforeObjects, afterObjects)
				}
				baselineTimes = append(baselineTimes, float64(baseline)/float64(time.Millisecond))
				finishTimes = append(finishTimes, float64(finished)/float64(time.Millisecond))
				data, _ := json.Marshal(map[string]any{"iteration": iteration + 1, "files": files, "bytes": total, "changedFiles": 20, "baselineMs": float64(baseline) / float64(time.Millisecond), "finishMs": float64(finished) / float64(time.Millisecond), "baselineLogicalBytes": beforeLogical, "baselineEncodedBytes": beforePhysical, "baselineObjects": beforeObjects, "incrementLogicalBytes": afterLogical - beforeLogical, "incrementEncodedBytes": afterPhysical - beforePhysical, "incrementObjects": afterObjects - beforeObjects})
				b.Log(string(data))
			}
			for phase, samples := range map[string][]float64{"baseline": baselineTimes, "finish": finishTimes} {
				sort.Float64s(samples)
				b.ReportMetric(samples[len(samples)/2], phase+"-median-ms")
				b.ReportMetric(samples[(len(samples)*95+99)/100-1], phase+"-p95-ms")
				b.ReportMetric(samples[len(samples)-1], phase+"-max-ms")
			}
		})
	}
}

func benchmarkGit(b *testing.B, root string, args ...string) {
	b.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	git, reason := resolveGit(ctx, "", normalizedLimits(Limits{}))
	if reason != "" {
		b.Fatal(reason)
	}
	if _, err := runGit(ctx, git, root, "", "", normalizedLimits(Limits{}), append([]string{"-c", "core.fsync="}, args...)...); err != nil {
		b.Fatal(err)
	}
}

func benchmarkStorage(b *testing.B, owned string) (int64, int64, int) {
	b.Helper()
	logical, physical, count := int64(0), int64(0), 0
	iterator, err := objectStore(owned).IterEncodedObjects(plumbing.AnyObject)
	if err != nil {
		b.Fatal(err)
	}
	err = iterator.ForEach(func(value plumbing.EncodedObject) error { logical += value.Size(); count++; return nil })
	iterator.Close()
	if err != nil {
		b.Fatal(err)
	}
	err = filepath.WalkDir(filepath.Join(repositoryPath(owned), "objects"), func(path string, item fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !item.IsDir() {
			info, err := item.Info()
			if err != nil {
				return err
			}
			physical += info.Size()
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return logical, physical, count
}

// BenchmarkTextDiffAtLimit 单独测量 PERF-06 的两侧 2 MiB 文本和五千行输出预算。
func BenchmarkTextDiffAtLimit(b *testing.B) {
	before, after := strings.Repeat("old\n", (2<<20)/4), strings.Repeat("new\n", (2<<20)/4)
	limits := normalizedLimits(Limits{})
	b.ReportAllocs()
	samples := make([]float64, 0, b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		plan := makeEdits(context.Background(), before, after, limits.DiffTimeout)
		hunks, truncated := makeHunks(plan.edits, limits)
		count := 0
		for _, hunk := range hunks {
			count += len(hunk.Lines)
		}
		if !plan.coarse || !truncated || count != limits.MaxDiffLines {
			b.Fatalf("diff budget not explicit: coarse=%v truncated=%v lines=%d", plan.coarse, truncated, count)
		}
		elapsed := float64(time.Since(started)) / float64(time.Millisecond)
		samples = append(samples, elapsed)
		b.StopTimer()
		b.Logf("iteration=%d bytesPerSide=%d algorithmMs=%.6f coarse=%t truncated=%t renderLines=%d", i+1, len(before), elapsed, plan.coarse, truncated, count)
		b.StartTimer()
	}
	b.StopTimer()
	sort.Float64s(samples)
	b.ReportMetric(samples[len(samples)/2], "median-ms")
	b.ReportMetric(samples[(len(samples)*95+99)/100-1], "p95-ms")
	b.ReportMetric(samples[len(samples)-1], "max-ms")
}

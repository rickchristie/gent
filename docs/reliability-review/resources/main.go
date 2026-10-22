//go:build gent_review_native_allocator && cgo && linux

// Run only on Linux with glibc. This diagnostic samples the process allocator and calls
// malloc_trim after close to distinguish unused resident pages from live native allocations.
// Gent does not call malloc_trim during normal use.
package main

/*
#include <malloc.h>
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rickchristie/gent/common"
	"github.com/rickchristie/gent/search"
)

func sample(label string) {
	debug.FreeOSMemory()
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		panic(err)
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	native := C.mallinfo2()
	fmt.Printf("%s heap=%d native_live=%d native_free=%d", label, stats.HeapAlloc,
		uint64(native.uordblks)+uint64(native.hblkhd), uint64(native.fordblks))
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "VmRSS:") || strings.HasPrefix(line, "Threads:") {
			fmt.Printf(" %s", strings.TrimSpace(line))
		}
	}
	fmt.Println()
}

func main() {
	cfg := common.FindConfig("multilingual-e5-small")
	dir, err := common.ModelDir(cfg.Model.Name)
	if err != nil {
		panic(err)
	}
	sample("before")
	for round := 0; round < 6; round++ {
		embedder, err := search.NewOnnxEmbedder(*cfg, search.OnnxOptions{
			ModelPath:     filepath.Join(dir, cfg.Model.ModelFile),
			TokenizerPath: filepath.Join(dir, "tokenizer.json"), NumThreads: 2, MaxConcurrency: 4,
		})
		if err != nil {
			panic(err)
		}
		for batch := 0; batch < 2; batch++ {
			var wg sync.WaitGroup
			for worker := 0; worker < 4; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < 25; i++ {
						if _, err := embedder.EmbedQuery(context.Background(),
							"Find the return policy for a damaged package."); err != nil {
							panic(err)
						}
					}
				}()
			}
			wg.Wait()
			sample(fmt.Sprintf("round=%d calls=%d", round, (batch+1)*100))
		}
		var before, after syscall.Rusage
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &before); err != nil {
			panic(err)
		}
		time.Sleep(500 * time.Millisecond)
		if err := syscall.Getrusage(syscall.RUSAGE_SELF, &after); err != nil {
			panic(err)
		}
		cpu := after.Utime.Nano() + after.Stime.Nano() - before.Utime.Nano() - before.Stime.Nano()
		fmt.Printf("round=%d idle_cpu_ms=%.3f\n", round, float64(cpu)/1e6)
		if err := embedder.Close(); err != nil {
			panic(err)
		}
		sample(fmt.Sprintf("round=%d closed", round))
		C.malloc_trim(0)
		sample(fmt.Sprintf("round=%d trimmed", round))
	}
}

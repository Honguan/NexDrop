package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	baseURL := flag.String("url", "http://127.0.0.1:8080", "NexDrop Node base URL")
	requests := flag.Int("requests", 1000, "number of requests")
	concurrency := flag.Int("concurrency", 10, "parallel requests")
	maximumP95 := flag.Duration("max-p95", 500*time.Millisecond, "maximum accepted p95")
	flag.Parse()
	if *requests < 1 || *concurrency < 1 {
		fmt.Fprintln(os.Stderr, "requests and concurrency must be positive")
		os.Exit(2)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	jobs := make(chan struct{})
	durations := make([]time.Duration, 0, *requests)
	var failures atomic.Int64
	var mutex sync.Mutex
	var workers sync.WaitGroup
	for range *concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range jobs {
				started := time.Now()
				response, err := client.Get(*baseURL + "/api/version")
				duration := time.Since(started)
				if err != nil || response.StatusCode != http.StatusOK {
					failures.Add(1)
				}
				if response != nil {
					_ = response.Body.Close()
				}
				mutex.Lock()
				durations = append(durations, duration)
				mutex.Unlock()
			}
		}()
	}
	for range *requests {
		jobs <- struct{}{}
	}
	close(jobs)
	workers.Wait()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95-1)/100]
	fmt.Printf("requests=%d concurrency=%d failures=%d p95=%s\n", *requests, *concurrency, failures.Load(), p95)
	if failures.Load() > 0 || p95 > *maximumP95 {
		os.Exit(1)
	}
}

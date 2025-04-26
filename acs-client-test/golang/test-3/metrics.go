package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	psnet "github.com/shirou/gopsutil/v3/net"
)

type OperationType string

const (
	PUT OperationType = "PUT"
	GET OperationType = "GET"
)

type OperationMetrics struct {
	TotalOperations uint64
	SuccessCount    uint64
	ErrorCount      uint64
	TotalBytes      uint64
	AvgLatency      time.Duration
	MaxLatency      time.Duration
	MinLatency      time.Duration
	AvgCompressTime time.Duration
	AvgTransferTime time.Duration
	Throughput      float64 // MB/s
}

type SystemMetrics struct {
	// Network metrics
	BytesSent         uint64
	BytesReceived     uint64
	PacketsSent       uint64
	PacketsReceived   uint64
	TCPRetransmits    uint64
	TCPConnections    int
	NetworkLatency    time.Duration
	NetworkThroughput float64 // MB/s

	// System metrics
	CPUUsage        float64 // percentage
	MemoryUsage     float64 // percentage
	MemoryAllocated uint64  // bytes
	GRPCActiveConns int

	// Operation-specific metrics
	PutMetrics OperationMetrics
	GetMetrics OperationMetrics
}

type MetricsCollector struct {
	stopChan  chan struct{}
	wg        sync.WaitGroup
	metrics   *SystemMetrics
	mu        sync.RWMutex
	startTime time.Time
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		stopChan:  make(chan struct{}),
		metrics:   &SystemMetrics{},
		startTime: time.Now(),
	}
}

func (mc *MetricsCollector) Start() {
	mc.wg.Add(3)

	// Collect network metrics
	go func() {
		defer mc.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		var lastBytesSent, lastBytesReceived uint64

		for {
			select {
			case <-mc.stopChan:
				return
			case <-ticker.C:
				stats, err := psnet.IOCounters(false)
				if err != nil {
					continue
				}
				if len(stats) > 0 {
					mc.mu.Lock()
					// Calculate throughput
					bytesSent := stats[0].BytesSent
					bytesReceived := stats[0].BytesRecv // Fixed: BytesReceived -> BytesRecv
					mc.metrics.NetworkThroughput = float64(bytesSent-lastBytesSent+bytesReceived-lastBytesReceived) / 1024 / 1024 // MB/s

					// Update metrics
					mc.metrics.BytesSent = bytesSent
					mc.metrics.BytesReceived = bytesReceived
					mc.metrics.PacketsSent = stats[0].PacketsSent
					mc.metrics.PacketsReceived = stats[0].PacketsRecv
					mc.mu.Unlock()

					lastBytesSent = bytesSent
					lastBytesReceived = bytesReceived
				}

				// Measure network latency
				if out, err := exec.Command("ping", "-c", "1", "acceleratedcloudstorageproduction.com").Output(); err == nil {
					parts := strings.Split(string(out), "time=")
					if len(parts) > 1 {
						timeStr := strings.Split(parts[1], " ")[0]
						if val, err := strconv.ParseFloat(timeStr, 64); err == nil {
							mc.mu.Lock()
							mc.metrics.NetworkLatency = time.Duration(val * float64(time.Millisecond))
							mc.mu.Unlock()
						}
					}
				}
			}
		}
	}()

	// Collect TCP metrics
	go func() {
		defer mc.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-mc.stopChan:
				return
			case <-ticker.C:
				// Get TCP retransmits
				out, err := exec.Command("netstat", "-s").Output()
				if err == nil {
					lines := strings.Split(string(out), "\n")
					for _, line := range lines {
						if strings.Contains(line, "segments retransmitted") {
							fields := strings.Fields(line)
							if len(fields) > 0 {
								if val, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
									mc.mu.Lock()
									mc.metrics.TCPRetransmits = val
									mc.mu.Unlock()
								}
							}
						}
					}
				}

				// Get active TCP connections
				conns, err := psnet.Connections("tcp")
				if err == nil {
					established := 0
					for _, conn := range conns {
						if conn.Status == "ESTABLISHED" {
							established++
						}
					}
					mc.mu.Lock()
					mc.metrics.TCPConnections = established
					mc.mu.Unlock()
				}
			}
		}
	}()

	// Collect system metrics
	go func() {
		defer mc.wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-mc.stopChan:
				return
			case <-ticker.C:
				// CPU usage
				if cpuPercent, err := cpu.Percent(time.Second, false); err == nil && len(cpuPercent) > 0 {
					mc.mu.Lock()
					mc.metrics.CPUUsage = cpuPercent[0]
					mc.mu.Unlock()
				}

				// Memory usage
				if vmStat, err := mem.VirtualMemory(); err == nil {
					mc.mu.Lock()
					mc.metrics.MemoryUsage = vmStat.UsedPercent
					mc.metrics.MemoryAllocated = vmStat.Used
					mc.mu.Unlock()
				}
			}
		}
	}()
}

func (mc *MetricsCollector) Stop() {
	close(mc.stopChan)
	mc.wg.Wait()
}

func (mc *MetricsCollector) RecordOperationMetrics(opType OperationType, start time.Time, compressStart, compressEnd, transferStart, transferEnd time.Time, byteCount uint64, err error) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	var metrics *OperationMetrics
	switch opType {
	case PUT:
		metrics = &mc.metrics.PutMetrics
	case GET:
		metrics = &mc.metrics.GetMetrics
	default:
		return
	}

	metrics.TotalOperations++
	if err != nil {
		metrics.ErrorCount++
	} else {
		metrics.SuccessCount++
	}

	latency := time.Since(start)
	metrics.TotalBytes += byteCount

	// Update latency stats
	if metrics.TotalOperations == 1 || latency < metrics.MinLatency {
		metrics.MinLatency = latency
	}
	if latency > metrics.MaxLatency {
		metrics.MaxLatency = latency
	}

	// Update average latencies
	count := float64(metrics.SuccessCount)
	metrics.AvgLatency = time.Duration((float64(metrics.AvgLatency)*float64(count-1) + float64(latency)) / float64(count))
	metrics.AvgCompressTime = time.Duration((float64(metrics.AvgCompressTime)*float64(count-1) + float64(compressEnd.Sub(compressStart))) / float64(count))
	metrics.AvgTransferTime = time.Duration((float64(metrics.AvgTransferTime)*float64(count-1) + float64(transferEnd.Sub(transferStart))) / float64(count))

	// Calculate throughput (MB/s)
	elapsedSeconds := time.Since(mc.startTime).Seconds()
	if elapsedSeconds > 0 {
		metrics.Throughput = float64(metrics.TotalBytes) / elapsedSeconds / 1024 / 1024
	}
}

func (mc *MetricsCollector) WriteResultsToFile(filename string) error {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	data, err := json.MarshalIndent(mc.metrics, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

func (mc *MetricsCollector) GetMetrics() SystemMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return *mc.metrics
}

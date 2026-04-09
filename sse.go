package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/sse"
)

// MetricEvent represents a snapshot of simulated server metrics.
type MetricEvent struct {
	Timestamp      time.Time `json:"timestamp" doc:"Time the metrics were sampled"`
	CPUPercent     float64   `json:"cpu_percent" doc:"CPU utilization percentage" minimum:"0" maximum:"100"`
	MemoryPercent  float64   `json:"memory_percent" doc:"Memory utilization percentage" minimum:"0" maximum:"100"`
	ActiveConns    int       `json:"active_connections" doc:"Number of active HTTP connections"`
	RequestsPerSec float64   `json:"requests_per_second" doc:"Inbound request rate"`
	Region         string    `json:"region" doc:"Data center region reporting metrics"`
}

// AlertEvent is emitted when a metric crosses a warning or critical threshold.
type AlertEvent struct {
	Timestamp time.Time `json:"timestamp" doc:"Time the alert was raised"`
	Severity  string    `json:"severity" enum:"warning,critical" doc:"Alert severity level"`
	Metric    string    `json:"metric" doc:"Name of the metric that triggered the alert"`
	Value     float64   `json:"value" doc:"Current value of the metric"`
	Threshold float64   `json:"threshold" doc:"Threshold that was exceeded"`
	Message   string    `json:"message" doc:"Human-readable alert description"`
}

var regions = []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}

// metricSimulator holds running state so values drift realistically between samples.
type metricSimulator struct {
	cpu       float64
	mem       float64
	conns     int
	rps       float64
	regionIdx int
}

func newMetricSimulator() *metricSimulator {
	return &metricSimulator{
		cpu:   55 + rand.Float64()*20,
		mem:   60 + rand.Float64()*15,
		conns: 80 + rand.Intn(40),
		rps:   300 + rand.Float64()*200,
	}
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

func (m *metricSimulator) next() MetricEvent {
	// Apply a random walk to each metric so the stream looks like real telemetry.
	m.cpu = clamp(m.cpu+rand.Float64()*20-10, 2, 98)
	m.mem = clamp(m.mem+rand.Float64()*10-5, 10, 95)
	m.conns += rand.Intn(21) - 10
	if m.conns < 0 {
		m.conns = 0
	}
	m.rps = clamp(m.rps+rand.Float64()*60-30, 0, 2000)

	return MetricEvent{
		Timestamp:      time.Now().UTC(),
		CPUPercent:     math.Round(m.cpu*10) / 10,
		MemoryPercent:  math.Round(m.mem*10) / 10,
		ActiveConns:    m.conns,
		RequestsPerSec: math.Round(m.rps*10) / 10,
		Region:         regions[m.regionIdx%len(regions)],
	}
}

func (s *APIServer) RegisterSSE(api huma.API) {
	sse.Register(api, huma.Operation{
		OperationID: "get-sse-metrics",
		Method:      http.MethodGet,
		Path:        "/sse/metrics",
		Summary:     "Stream server metrics",
		Description: "Streams simulated server metrics as a [Server-Sent Events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events) (SSE) stream. Each event is a JSON object with CPU, memory, connection, and request-rate fields sampled from a random walk to mimic real telemetry.",
		Tags:        []string{"SSE"},
	}, map[string]any{
		"metrics": MetricEvent{},
		"alert":   AlertEvent{},
	}, func(ctx context.Context, input *struct {
		Count int `query:"count" minimum:"1" maximum:"100" default:"10" doc:"Number of metric events to emit before closing the stream"`
	}, send sse.Sender) {
		count := input.Count
		if count == 0 {
			count = 10
		}
		sim := newMetricSimulator()

		for i := 0; i < count; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			event := sim.next()
			if err := send.Data(event); err != nil {
				return
			}

			// Emit alert events when metrics cross thresholds.
			now := event.Timestamp
			type check struct {
				metric   string
				value    float64
				warning  float64
				critical float64
				unit     string
			}
			checks := []check{
				{"cpu_percent", event.CPUPercent, 60, 80, "%"},
				{"memory_percent", event.MemoryPercent, 65, 80, "%"},
			}
			for _, c := range checks {
				var severity, msg string
				switch {
				case c.value >= c.critical:
					severity = "critical"
					msg = fmt.Sprintf("%s is critically high at %.1f%s (threshold: %.0f%s)", c.metric, c.value, c.unit, c.critical, c.unit)
				case c.value >= c.warning:
					severity = "warning"
					msg = fmt.Sprintf("%s is elevated at %.1f%s (threshold: %.0f%s)", c.metric, c.value, c.unit, c.warning, c.unit)
				}
				if severity != "" {
					threshold := c.warning
					if severity == "critical" {
						threshold = c.critical
					}
					if err := send(sse.Message{Data: AlertEvent{
						Timestamp: now,
						Severity:  severity,
						Metric:    c.metric,
						Value:     c.value,
						Threshold: threshold,
						Message:   msg,
					}}); err != nil {
						return
					}
				}
			}

			if i < count-1 {
				// Wait 500 ms – 2 s before the next event.
				delay := time.Duration(500+rand.Intn(1500)) * time.Millisecond
				select {
				case <-ctx.Done():
					return
				case <-time.After(delay):
				}
			}
		}
	})
}

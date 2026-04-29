package main

import (
	"context"
	"encoding/json"
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

// DocsUser identifies a user in docs-oriented stream examples.
type DocsUser struct {
	ID   string `json:"id" doc:"User ID"`
	Name string `json:"name" doc:"Display name"`
}

// DocsEvent is a simple event shape for streaming documentation examples.
type DocsEvent struct {
	Type      string    `json:"type" enum:"login,update,logout" doc:"Event type"`
	User      DocsUser  `json:"user" doc:"User associated with the event"`
	Message   string    `json:"message" doc:"Human-readable message"`
	Timestamp time.Time `json:"timestamp" doc:"Event timestamp"`
}

var regions = []string{"us-east-1", "us-west-2", "eu-west-1", "ap-southeast-1"}
var docsEventTypes = []string{"login", "update", "logout"}
var docsUsers = []DocsUser{
	{ID: "u_123", Name: "Alice"},
	{ID: "u_456", Name: "Sam"},
	{ID: "u_789", Name: "Morgan"},
}

func docsEvent(i int) DocsEvent {
	user := docsUsers[i%len(docsUsers)]
	eventType := docsEventTypes[i%len(docsEventTypes)]
	return DocsEvent{
		Type:      eventType,
		User:      user,
		Message:   fmt.Sprintf("%s event for %s", eventType, user.Name),
		Timestamp: time.Now().UTC(),
	}
}

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

	region := regions[m.regionIdx%len(regions)]
	m.regionIdx = (m.regionIdx + 1) % len(regions)

	return MetricEvent{
		Timestamp:      time.Now().UTC(),
		CPUPercent:     math.Round(m.cpu*10) / 10,
		MemoryPercent:  math.Round(m.mem*10) / 10,
		ActiveConns:    m.conns,
		RequestsPerSec: math.Round(m.rps*10) / 10,
		Region:         region,
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

func (s *APIServer) RegisterEvents(api huma.API) {
	sse.Register(api, huma.Operation{
		OperationID: "get-events",
		Method:      http.MethodGet,
		Path:        "/events",
		Summary:     "Stream simple docs events",
		Description: "Streams a bounded Server-Sent Events feed with a simple `type`, `user.id`, `message`, and `timestamp` shape for documentation examples.",
		Tags:        []string{"SSE"},
	}, map[string]any{
		"event": DocsEvent{},
	}, func(ctx context.Context, input *struct {
		Count int `query:"count" minimum:"1" maximum:"100" default:"10" doc:"Number of events to emit"`
	}, send sse.Sender) {
		for i := 0; i < input.Count; i++ {
			if err := send.Data(docsEvent(i)); err != nil {
				return
			}
			if i < input.Count-1 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(250 * time.Millisecond):
				}
			}
		}
	})
}

func (s *APIServer) RegisterLogs(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "get-logs",
		Method:      http.MethodGet,
		Path:        "/logs",
		Summary:     "Stream newline-delimited JSON logs",
		Tags:        []string{"Streaming"},
	}, func(_ context.Context, input *struct {
		Count int `query:"count" minimum:"1" maximum:"100" default:"10" doc:"Number of log lines to emit"`
	}) (*huma.StreamResponse, error) {
		return &huma.StreamResponse{
			Body: func(streamCtx huma.Context) {
				streamCtx.SetHeader("Content-Type", "application/x-ndjson")
				bw := streamCtx.BodyWriter()
				enc := json.NewEncoder(bw)
				for i := 0; i < input.Count; i++ {
					if err := enc.Encode(docsEvent(i)); err != nil {
						return
					}
					if f, ok := bw.(http.Flusher); ok {
						f.Flush()
					}
					if i < input.Count-1 {
						select {
						case <-streamCtx.Context().Done():
							return
						case <-time.After(250 * time.Millisecond):
						}
					}
				}
			},
		}, nil
	})
}

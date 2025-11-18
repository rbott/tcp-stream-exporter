package statistics

import (
	"fmt"
	"math"
	"tcp-stream-exporter/types"
	"testing"
	"time"
)

func TestCalculateHealthScore(t *testing.T) {
	tests := []struct {
		name     string
		stream   *types.TCPStream
		expected float64
	}{
		{
			name: "perfect stream",
			stream: &types.TCPStream{
				Retransmits:       0,
				Resets:            0,
				OutOfOrderPackets: 0,
				DuplicateAcks:     0,
				ZeroWindowCount:   0,
				GapDetections:     0,
				BitrateHistory:    []float64{},
				Delays:            []time.Duration{},
			},
			expected: 100.0,
		},
		{
			name: "stream with retransmits",
			stream: &types.TCPStream{
				Retransmits:       3,
				Resets:            0,
				OutOfOrderPackets: 0,
				DuplicateAcks:     0,
				ZeroWindowCount:   0,
				GapDetections:     0,
				BitrateHistory:    []float64{},
				Delays:            []time.Duration{},
			},
			expected: 85.0, // 100 - (3 * 5)
		},
		{
			name: "stream with resets",
			stream: &types.TCPStream{
				Retransmits:       0,
				Resets:            2,
				OutOfOrderPackets: 0,
				DuplicateAcks:     0,
				ZeroWindowCount:   0,
				GapDetections:     0,
				BitrateHistory:    []float64{},
				Delays:            []time.Duration{},
			},
			expected: 60.0, // 100 - (2 * 20)
		},
		{
			name: "stream with max retransmits penalty",
			stream: &types.TCPStream{
				Retransmits:       10,
				Resets:            0,
				OutOfOrderPackets: 0,
				DuplicateAcks:     0,
				ZeroWindowCount:   0,
				GapDetections:     0,
				BitrateHistory:    []float64{},
				Delays:            []time.Duration{},
			},
			expected: 70.0, // 100 - 30 (max penalty for retransmits)
		},
		{
			name: "stream with all problems",
			stream: &types.TCPStream{
				Retransmits:       10,
				Resets:            2,
				OutOfOrderPackets: 15,
				DuplicateAcks:     15,
				ZeroWindowCount:   5,
				GapDetections:     10,
				BitrateHistory:    []float64{},
				Delays:            []time.Duration{},
			},
			expected: 0.0, // Should cap at 0
		},
		{
			name: "stream with high bitrate variance",
			stream: &types.TCPStream{
				Retransmits:       0,
				Resets:            0,
				OutOfOrderPackets: 0,
				DuplicateAcks:     0,
				ZeroWindowCount:   0,
				GapDetections:     0,
				BitrateHistory:    []float64{100, 1000, 50, 2000, 30, 1500},
				Delays:            []time.Duration{},
			},
			expected: 90.0, // 100 - 10 (CV > 0.3)
		},
		{
			name: "stream with high jitter",
			stream: &types.TCPStream{
				Retransmits:       0,
				Resets:            0,
				OutOfOrderPackets: 0,
				DuplicateAcks:     0,
				ZeroWindowCount:   0,
				GapDetections:     0,
				BitrateHistory:    []float64{},
				Delays: []time.Duration{
					10 * time.Millisecond,
					200 * time.Millisecond,
					15 * time.Millisecond,
					180 * time.Millisecond,
					20 * time.Millisecond,
					150 * time.Millisecond,
				},
			},
			expected: 95.0, // 100 - 5 (jitter > 50ms)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateHealthScore(tt.stream)
			if math.Abs(result-tt.expected) > 0.1 {
				t.Errorf("CalculateHealthScore() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateAvgDelay(t *testing.T) {
	tests := []struct {
		name     string
		delays   []time.Duration
		expected time.Duration
	}{
		{
			name:     "empty slice",
			delays:   []time.Duration{},
			expected: 0,
		},
		{
			name:     "single value",
			delays:   []time.Duration{100 * time.Millisecond},
			expected: 100 * time.Millisecond,
		},
		{
			name: "multiple values",
			delays: []time.Duration{
				100 * time.Millisecond,
				200 * time.Millisecond,
				300 * time.Millisecond,
			},
			expected: 200 * time.Millisecond,
		},
		{
			name: "varying values",
			delays: []time.Duration{
				50 * time.Millisecond,
				150 * time.Millisecond,
			},
			expected: 100 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateAvgDelay(tt.delays)
			if result != tt.expected {
				t.Errorf("CalculateAvgDelay() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateJitter(t *testing.T) {
	tests := []struct {
		name     string
		delays   []time.Duration
		expected time.Duration
	}{
		{
			name:     "empty slice",
			delays:   []time.Duration{},
			expected: 0,
		},
		{
			name:     "single value",
			delays:   []time.Duration{100 * time.Millisecond},
			expected: 0,
		},
		{
			name: "identical values",
			delays: []time.Duration{
				100 * time.Millisecond,
				100 * time.Millisecond,
				100 * time.Millisecond,
			},
			expected: 0,
		},
		{
			name: "varying values",
			delays: []time.Duration{
				100 * time.Millisecond,
				200 * time.Millisecond,
			},
			expected: 50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateJitter(tt.delays)
			// Allow small floating point differences
			diff := result - tt.expected
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Microsecond {
				t.Errorf("CalculateJitter() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateCV(t *testing.T) {
	tests := []struct {
		name     string
		values   []float64
		expected float64
	}{
		{
			name:     "empty slice",
			values:   []float64{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []float64{100},
			expected: 0,
		},
		{
			name:     "identical values",
			values:   []float64{100, 100, 100},
			expected: 0,
		},
		{
			name:     "mean is zero",
			values:   []float64{-5, 0, 5},
			expected: 0,
		},
		{
			name:     "varying values",
			values:   []float64{10, 20, 30},
			expected: 0.4082, // Approximate CV
		},
		{
			name:     "high variance",
			values:   []float64{1, 100, 1000},
			expected: 1.2245, // Approximate CV
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateCV(tt.values)
			if math.Abs(result-tt.expected) > 0.001 {
				fmt.Printf("result = %v, expected = %v, diff =%v\n", result, tt.expected, math.Abs(result-tt.expected))
				t.Errorf("CalculateCV() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateIntCV(t *testing.T) {
	tests := []struct {
		name     string
		values   []int
		expected float64
	}{
		{
			name:     "empty slice",
			values:   []int{},
			expected: 0,
		},
		{
			name:     "single value",
			values:   []int{100},
			expected: 0,
		},
		{
			name:     "identical values",
			values:   []int{50, 50, 50},
			expected: 0,
		},
		{
			name:     "varying values",
			values:   []int{10, 20, 30},
			expected: 0.4082, // Approximate CV
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateIntCV(tt.values)
			if math.Abs(result-tt.expected) > 0.001 {
				t.Errorf("CalculateIntCV() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCalculateDurationVariance(t *testing.T) {
	tests := []struct {
		name      string
		durations []time.Duration
		expected  time.Duration
	}{
		{
			name:      "empty slice",
			durations: []time.Duration{},
			expected:  0,
		},
		{
			name:      "single value",
			durations: []time.Duration{100 * time.Millisecond},
			expected:  0,
		},
		{
			name: "identical values",
			durations: []time.Duration{
				100 * time.Millisecond,
				100 * time.Millisecond,
				100 * time.Millisecond,
			},
			expected: 0,
		},
		{
			name: "varying values",
			durations: []time.Duration{
				100 * time.Millisecond,
				200 * time.Millisecond,
			},
			expected: 50 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateDurationVariance(tt.durations)
			// Allow small floating point differences
			diff := result - tt.expected
			if diff < 0 {
				diff = -diff
			}
			if diff > time.Microsecond {
				t.Errorf("CalculateDurationVariance() = %v, want %v", result, tt.expected)
			}
		})
	}
}

// Benchmark tests
func BenchmarkCalculateHealthScore(b *testing.B) {
	stream := &types.TCPStream{
		Retransmits:       5,
		Resets:            1,
		OutOfOrderPackets: 3,
		DuplicateAcks:     2,
		ZeroWindowCount:   1,
		GapDetections:     2,
		BitrateHistory:    []float64{100, 200, 150, 180, 220, 190},
		Delays: []time.Duration{
			10 * time.Millisecond,
			20 * time.Millisecond,
			15 * time.Millisecond,
			25 * time.Millisecond,
			18 * time.Millisecond,
			22 * time.Millisecond,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalculateHealthScore(stream)
	}
}

func BenchmarkCalculateCV(b *testing.B) {
	values := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CalculateCV(values)
	}
}

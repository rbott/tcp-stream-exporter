package statistics

import (
	"math"
	"tcp-stream-exporter/types"
	"time"
)

func CalculateHealthScore(stream *types.TCPStream) float64 {
	score := 100.0
	score -= math.Min(float64(stream.Retransmits)*5, 30)
	score -= float64(stream.Resets) * 20
	score -= math.Min(float64(stream.OutOfOrderPackets)*2, 20)
	score -= math.Min(float64(stream.DuplicateAcks)*1, 10)
	score -= math.Min(float64(stream.ZeroWindowCount)*10, 20)
	score -= math.Min(float64(stream.GapDetections)*3, 15)

	if len(stream.BitrateHistory) > 5 {
		cv := CalculateCV(stream.BitrateHistory)
		if cv > 0.3 {
			score -= 10
		}
	}

	if len(stream.Delays) > 5 {
		jitter := CalculateJitter(stream.Delays)
		if jitter > 50*time.Millisecond {
			score -= 5
		}
	}

	if score < 0 {
		score = 0
	}
	return score
}

func CalculateAvgDelay(delays []time.Duration) time.Duration {
	if len(delays) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range delays {
		sum += d
	}
	return sum / time.Duration(len(delays))
}

func CalculateJitter(delays []time.Duration) time.Duration {
	if len(delays) < 2 {
		return 0
	}
	avg := CalculateAvgDelay(delays)
	var variance float64
	for _, d := range delays {
		diff := float64(d - avg)
		variance += diff * diff
	}
	variance /= float64(len(delays))
	return time.Duration(math.Sqrt(variance))
}

func CalculateCV(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	mean := 0.0
	for _, v := range values {
		mean += v
	}
	mean /= float64(len(values))
	if mean == 0 {
		return 0
	}
	variance := 0.0
	for _, v := range values {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(values))
	return math.Sqrt(variance) / mean
}

func CalculateIntCV(values []int) float64 {
	floatValues := make([]float64, len(values))
	for i, v := range values {
		floatValues[i] = float64(v)
	}
	return CalculateCV(floatValues)
}

func CalculateDurationVariance(durations []time.Duration) time.Duration {
	if len(durations) < 2 {
		return 0
	}
	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	mean := sum / time.Duration(len(durations))
	var variance float64
	for _, d := range durations {
		diff := float64(d - mean)
		variance += diff * diff
	}
	variance /= float64(len(durations))
	return time.Duration(math.Sqrt(variance))
}

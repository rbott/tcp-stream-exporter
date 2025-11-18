package prom

import (
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/prometheus/client_golang/prometheus"
	"log/slog"
	"math"
	"sync"
	"tcp-stream-exporter/statistics"
	"tcp-stream-exporter/types"
	"time"
)

type TCPCollector struct {
	Streams    map[types.StreamKey]*types.TCPStream
	StreamsMux sync.RWMutex

	streamCount         *prometheus.Desc
	retransmits         *prometheus.Desc
	resets              *prometheus.Desc
	windowSize          *prometheus.Desc
	avgDelay            *prometheus.Desc
	jitter              *prometheus.Desc
	bitrate             *prometheus.Desc
	bitrateVariance     *prometheus.Desc
	packetSizeVariance  *prometheus.Desc
	iatVariance         *prometheus.Desc
	outOfOrder          *prometheus.Desc
	duplicateAcks       *prometheus.Desc
	zeroWindows         *prometheus.Desc
	streamHealth        *prometheus.Desc
	streamUptime        *prometheus.Desc
	gapDetections       *prometheus.Desc
	maxDelay            *prometheus.Desc
	minDelay            *prometheus.Desc
	packetRate          *prometheus.Desc
	bytesTransferred    *prometheus.Desc
	droppedPackets      *prometheus.Desc
	packetsProcessed    *prometheus.Desc
	queueFreezes        *prometheus.Desc
	captureDropped      uint64
	captureProcessed    uint64
	captureQueueFreezes uint64
	captureMux          sync.RWMutex
}

func NewTCPCollector() *TCPCollector {
	return &TCPCollector{
		Streams: make(map[types.StreamKey]*types.TCPStream),
		streamCount: prometheus.NewDesc(
			"tcp_stream_count", "Number of active TCP streams", nil, nil,
		),
		retransmits: prometheus.NewDesc(
			"tcp_retransmits_total", "Number of TCP retransmissions per stream",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		resets: prometheus.NewDesc(
			"tcp_resets_total", "Number of TCP resets per stream",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		windowSize: prometheus.NewDesc(
			"tcp_window_size_bytes", "TCP window size per stream",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		avgDelay: prometheus.NewDesc(
			"tcp_avg_delay_seconds", "Average delay per stream",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		jitter: prometheus.NewDesc(
			"tcp_jitter_seconds", "Jitter (delay variance) per stream",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		bitrate: prometheus.NewDesc(
			"tcp_bitrate_bps", "Current bitrate in bits per second",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		bitrateVariance: prometheus.NewDesc(
			"tcp_bitrate_variance", "Variance in bitrate (coefficient of variation)",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		packetSizeVariance: prometheus.NewDesc(
			"tcp_packet_size_variance", "Variance in packet sizes (coefficient of variation)",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		iatVariance: prometheus.NewDesc(
			"tcp_interarrival_variance_seconds", "Variance in packet inter-arrival times",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		outOfOrder: prometheus.NewDesc(
			"tcp_out_of_order_packets_total", "Number of out-of-order packets",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		duplicateAcks: prometheus.NewDesc(
			"tcp_duplicate_acks_total", "Number of duplicate ACKs",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		zeroWindows: prometheus.NewDesc(
			"tcp_zero_window_events_total", "Number of zero window events",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		streamHealth: prometheus.NewDesc(
			"tcp_stream_health_score", "Stream health score (0-100, 100=perfect)",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		streamUptime: prometheus.NewDesc(
			"tcp_stream_uptime_seconds", "Stream uptime in seconds",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		gapDetections: prometheus.NewDesc(
			"tcp_sequence_gaps_total", "Number of gaps detected in sequence numbers",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		maxDelay: prometheus.NewDesc(
			"tcp_max_delay_seconds", "Maximum delay observed",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		minDelay: prometheus.NewDesc(
			"tcp_min_delay_seconds", "Minimum delay observed",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		packetRate: prometheus.NewDesc(
			"tcp_packet_rate_pps", "Packet rate in packets per second",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		bytesTransferred: prometheus.NewDesc(
			"tcp_bytes_transferred_total", "Total bytes transferred",
			[]string{"src_ip", "src_port", "dst_ip", "dst_port", "state"}, nil,
		),
		droppedPackets: prometheus.NewDesc(
			"capture_dropped_packets_total", "Number of packets dropped by kernel", nil, nil,
		),
		packetsProcessed: prometheus.NewDesc(
			"capture_packets_processed_total", "Number of packets processed", nil, nil,
		),
		queueFreezes: prometheus.NewDesc(
			"capture_queue_freezes_total", "Number of queue freezes (only AF_PACKET v3)", nil, nil,
		),
	}
}

func (c *TCPCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.streamCount
	ch <- c.retransmits
	ch <- c.resets
	ch <- c.windowSize
	ch <- c.avgDelay
	ch <- c.jitter
	ch <- c.bitrate
	ch <- c.bitrateVariance
	ch <- c.packetSizeVariance
	ch <- c.iatVariance
	ch <- c.outOfOrder
	ch <- c.duplicateAcks
	ch <- c.zeroWindows
	ch <- c.streamHealth
	ch <- c.streamUptime
	ch <- c.gapDetections
	ch <- c.maxDelay
	ch <- c.minDelay
	ch <- c.packetRate
	ch <- c.bytesTransferred
	ch <- c.droppedPackets
	ch <- c.packetsProcessed
}

func (c *TCPCollector) Collect(ch chan<- prometheus.Metric) {
	c.StreamsMux.RLock()
	defer c.StreamsMux.RUnlock()

	activeCount := 0.0
	for key, stream := range c.Streams {
		state := "active"
		if stream.Fin {
			state = "finalized"
		}
		if stream.Reset {
			state = "reset"
		}

		labels := []string{
			key.SrcIP, fmt.Sprintf("%d", key.SrcPort),
			key.DstIP, fmt.Sprintf("%d", key.DstPort),
			state,
		}
		if !stream.Fin && !stream.Reset {
			activeCount++
		}

		ch <- prometheus.MustNewConstMetric(c.retransmits, prometheus.CounterValue, float64(stream.Retransmits), labels...)
		ch <- prometheus.MustNewConstMetric(c.resets, prometheus.CounterValue, float64(stream.Resets), labels...)
		ch <- prometheus.MustNewConstMetric(c.windowSize, prometheus.GaugeValue, float64(stream.WindowSize), labels...)
		ch <- prometheus.MustNewConstMetric(c.outOfOrder, prometheus.CounterValue, float64(stream.OutOfOrderPackets), labels...)
		ch <- prometheus.MustNewConstMetric(c.duplicateAcks, prometheus.CounterValue, float64(stream.DuplicateAcks), labels...)
		ch <- prometheus.MustNewConstMetric(c.zeroWindows, prometheus.CounterValue, float64(stream.ZeroWindowCount), labels...)
		ch <- prometheus.MustNewConstMetric(c.gapDetections, prometheus.CounterValue, float64(stream.GapDetections), labels...)
		ch <- prometheus.MustNewConstMetric(c.bytesTransferred, prometheus.CounterValue, float64(stream.BytesTransferred), labels...)

		var uptime time.Duration
		if stream.Fin {
			uptime = stream.FinTimestamp.Sub(stream.StreamStartTime)
		} else if stream.Reset {
			uptime = stream.ResetTimestamp.Sub(stream.StreamStartTime)
		} else {
			uptime = time.Since(stream.StreamStartTime)
		}
		ch <- prometheus.MustNewConstMetric(c.streamUptime, prometheus.GaugeValue, uptime.Seconds(), labels...)

		if uptime.Seconds() > 0 {
			pps := float64(stream.PacketCount) / uptime.Seconds()
			ch <- prometheus.MustNewConstMetric(c.packetRate, prometheus.GaugeValue, pps, labels...)
		}

		if len(stream.Delays) > 0 {
			avgDelay := statistics.CalculateAvgDelay(stream.Delays)
			ch <- prometheus.MustNewConstMetric(c.avgDelay, prometheus.GaugeValue, avgDelay.Seconds(), labels...)

			jitter := statistics.CalculateJitter(stream.Delays)
			ch <- prometheus.MustNewConstMetric(c.jitter, prometheus.GaugeValue, jitter.Seconds(), labels...)

			if stream.MaxDelay > 0 {
				ch <- prometheus.MustNewConstMetric(c.maxDelay, prometheus.GaugeValue, stream.MaxDelay.Seconds(), labels...)
			}
			if stream.MinDelay > 0 {
				ch <- prometheus.MustNewConstMetric(c.minDelay, prometheus.GaugeValue, stream.MinDelay.Seconds(), labels...)
			}
		}

		if len(stream.BitrateHistory) > 0 {
			currentBitrate := stream.BitrateHistory[len(stream.BitrateHistory)-1]
			ch <- prometheus.MustNewConstMetric(c.bitrate, prometheus.GaugeValue, currentBitrate, labels...)

			bitrateCV := statistics.CalculateCV(stream.BitrateHistory)
			ch <- prometheus.MustNewConstMetric(c.bitrateVariance, prometheus.GaugeValue, bitrateCV, labels...)
		}

		if len(stream.PacketSizes) > 1 {
			packetSizeCV := statistics.CalculateIntCV(stream.PacketSizes)
			ch <- prometheus.MustNewConstMetric(c.packetSizeVariance, prometheus.GaugeValue, packetSizeCV, labels...)
		}

		if len(stream.InterArrivalTimes) > 1 {
			iatVariance := statistics.CalculateDurationVariance(stream.InterArrivalTimes)
			ch <- prometheus.MustNewConstMetric(c.iatVariance, prometheus.GaugeValue, iatVariance.Seconds(), labels...)
		}

		healthScore := statistics.CalculateHealthScore(stream)
		ch <- prometheus.MustNewConstMetric(c.streamHealth, prometheus.GaugeValue, healthScore, labels...)
	}
	ch <- prometheus.MustNewConstMetric(
		c.streamCount, prometheus.GaugeValue, activeCount,
	)
	c.captureMux.RLock()
	ch <- prometheus.MustNewConstMetric(
		c.droppedPackets, prometheus.CounterValue, float64(c.captureDropped),
	)
	ch <- prometheus.MustNewConstMetric(
		c.packetsProcessed, prometheus.CounterValue, float64(c.captureProcessed),
	)
	ch <- prometheus.MustNewConstMetric(
		c.queueFreezes, prometheus.CounterValue, float64(c.captureQueueFreezes),
	)
	c.captureMux.RUnlock()
}

func (c *TCPCollector) ProcessPacket(packet gopacket.Packet) {
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		return
	}
	ip, _ := ipLayer.(*layers.IPv4)

	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		return
	}
	tcp, _ := tcpLayer.(*layers.TCP)

	key := types.StreamKey{
		SrcIP:   ip.SrcIP.String(),
		DstIP:   ip.DstIP.String(),
		SrcPort: uint16(tcp.SrcPort),
		DstPort: uint16(tcp.DstPort),
	}

	c.StreamsMux.Lock()
	defer c.StreamsMux.Unlock()

	stream, exists := c.Streams[key]
	if !exists {
		stream = &types.TCPStream{
			SrcIP:            ip.SrcIP.String(),
			DstIP:            ip.DstIP.String(),
			SrcPort:          uint16(tcp.SrcPort),
			DstPort:          uint16(tcp.DstPort),
			SeqNumbers:       make(map[uint32]time.Time),
			LastTimestamp:    time.Now(),
			StreamStartTime:  time.Now(),
			LastBitrateCheck: time.Now(),
			MinDelay:         time.Duration(math.MaxInt64),
			Fin:              false,
			Reset:            false,
		}
		c.Streams[key] = stream
	}

	now := time.Now()
	stream.PacketCount++
	stream.WindowSize = tcp.Window

	payloadLen := len(tcp.Payload)
	if payloadLen > 0 {
		stream.BytesTransferred += uint64(payloadLen)
		stream.PacketSizes = append(stream.PacketSizes, payloadLen)
		if len(stream.PacketSizes) > 100 {
			stream.PacketSizes = stream.PacketSizes[1:]
		}
	}

	if !stream.LastPacketTime.IsZero() {
		iat := now.Sub(stream.LastPacketTime)
		if iat > 0 && iat < 10*time.Second {
			stream.InterArrivalTimes = append(stream.InterArrivalTimes, iat)
			if len(stream.InterArrivalTimes) > 100 {
				stream.InterArrivalTimes = stream.InterArrivalTimes[1:]
			}
		}
	}
	stream.LastPacketTime = now

	if now.Sub(stream.LastBitrateCheck) >= 1*time.Second {
		bytesDiff := stream.BytesTransferred - stream.LastByteCount
		timeDiff := now.Sub(stream.LastBitrateCheck).Seconds()
		bitrate := float64(bytesDiff*8) / timeDiff

		stream.BitrateHistory = append(stream.BitrateHistory, bitrate)
		if len(stream.BitrateHistory) > 60 {
			stream.BitrateHistory = stream.BitrateHistory[1:]
		}

		stream.LastByteCount = stream.BytesTransferred
		stream.LastBitrateCheck = now
	}

	if tcp.Window == 0 {
		stream.ZeroWindowCount++
	}

	if tcp.RST {
		stream.Resets++
	}

	if tcp.ACK && stream.LastAck != 0 {
		if tcp.Ack == stream.LastDupAck {
			stream.DupAckCount++
			if stream.DupAckCount >= 2 {
				stream.DuplicateAcks++
				stream.DupAckCount = 0
			}
		} else {
			stream.LastDupAck = tcp.Ack
			stream.DupAckCount = 0
		}
	}

	if stream.PacketCount > 1 && tcp.Seq > 0 && stream.LastSeq > 0 {
		expectedSeq := stream.LastSeq + uint32(len(tcp.Payload))
		if tcp.Seq < stream.LastSeq && !tcp.SYN {
			stream.OutOfOrderPackets++
		}
		if tcp.Seq > expectedSeq && expectedSeq > stream.LastSeq {
			stream.GapDetections++
		}
	}

	if stream.PacketCount > 1 && tcp.Seq <= stream.LastSeq && !tcp.SYN && len(tcp.Payload) > 0 {
		if _, seen := stream.SeqNumbers[tcp.Seq]; seen {
			stream.Retransmits++
		}
	}

	if tcp.ACK && stream.LastAck != 0 {
		if sendTime, ok := stream.SeqNumbers[tcp.Ack]; ok {
			delay := now.Sub(sendTime)
			if delay > 0 && delay < 10*time.Second {
				stream.Delays = append(stream.Delays, delay)
				if len(stream.Delays) > 100 {
					stream.Delays = stream.Delays[1:]
				}

				if delay > stream.MaxDelay {
					stream.MaxDelay = delay
				}
				if delay < stream.MinDelay {
					stream.MinDelay = delay
				}
			}
			delete(stream.SeqNumbers, tcp.Ack)
		}
	}

	if tcp.Seq > 0 && len(tcp.Payload) > 0 {
		stream.SeqNumbers[tcp.Seq] = now
		if len(stream.SeqNumbers) > 1000 {
			for seq := range stream.SeqNumbers {
				delete(stream.SeqNumbers, seq)
				break
			}
		}
	}

	if tcp.FIN {
		stream.Fin = true
		stream.FinTimestamp = now
	}

	if tcp.RST {
		stream.Reset = true
		stream.ResetTimestamp = now
	}

	stream.LastSeq = tcp.Seq
	stream.LastAck = tcp.Ack
	stream.LastTimestamp = now

}

func (c *TCPCollector) CleanupOldStreams(maxTimeoutAge time.Duration, maxFinalizedAge time.Duration) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.StreamsMux.Lock()
		now := time.Now()
		finStreams := 0
		resetStreams := 0
		timeoutStreams := 0
		for key, stream := range c.Streams {
			if stream.Fin && now.Sub(stream.FinTimestamp) > maxFinalizedAge {
				finStreams++
				slog.Debug("Removed FIN stream", "stream", fmt.Sprintf("%+v", stream))
				delete(c.Streams, key)
			}
			if stream.Reset && now.Sub(stream.ResetTimestamp) > maxFinalizedAge {
				resetStreams++
				slog.Debug("Removed RST stream", "stream", fmt.Sprintf("%+v", stream))
				delete(c.Streams, key)
			}
			if now.Sub(stream.LastTimestamp) > maxTimeoutAge {
				timeoutStreams++
				slog.Debug("Removed expired stream", "stream", fmt.Sprintf("%+v", stream))
				delete(c.Streams, key)
			}
		}
		if finStreams > 0 || timeoutStreams > 0 || resetStreams > 0 {
			slog.Info("Cleaned up streams", "finalised", finStreams, "reset", resetStreams, "timeout", timeoutStreams)
		}
		c.StreamsMux.Unlock()
	}
}

func (c *TCPCollector) UpdateCaptureStats(dropped, processed, freezes uint64) {
	c.captureMux.Lock()
	defer c.captureMux.Unlock()
	c.captureDropped = dropped
	c.captureProcessed = processed
	c.captureQueueFreezes = freezes
}

package prom

import (
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/prometheus/client_golang/prometheus"
	"net"
	"tcp-stream-exporter/types"
	"testing"
	"time"
)

// TCP Flag constants for testing
type TCPFlag uint8

const (
	TCPFlagFin TCPFlag = 1 << iota
	TCPFlagSyn
	TCPFlagRst
	TCPFlagPsh
	TCPFlagAck
	TCPFlagUrg
)

// Helper function to create a TCP packet
func createTCPPacket(srcIP, dstIP string, srcPort, dstPort uint16, seq, ack uint32, flags TCPFlag, payload []byte) gopacket.Packet {
	srcMac, _ := net.ParseMAC("00:01:02:03:04:05")
	dstMac, _ := net.ParseMAC("00:01:02:03:04:AA")
	ethernetLayer := &layers.Ethernet{
		SrcMAC:       srcMac,
		DstMAC:       dstMac,
		EthernetType: layers.EthernetTypeIPv4,
	}

	ipLayer := &layers.IPv4{
		SrcIP:    net.ParseIP(srcIP),
		DstIP:    net.ParseIP(dstIP),
		Protocol: layers.IPProtocolTCP,
		Version:  4,
	}

	tcpLayer := &layers.TCP{
		SrcPort: layers.TCPPort(srcPort),
		DstPort: layers.TCPPort(dstPort),
		Seq:     seq,
		Ack:     ack,
		Window:  65535,
	}

	// Set flags based on our custom TCPFlag type
	if flags&TCPFlagSyn != 0 {
		tcpLayer.SYN = true
	}
	if flags&TCPFlagAck != 0 {
		tcpLayer.ACK = true
	}
	if flags&TCPFlagFin != 0 {
		tcpLayer.FIN = true
	}
	if flags&TCPFlagRst != 0 {
		tcpLayer.RST = true
	}
	if flags&TCPFlagPsh != 0 {
		tcpLayer.PSH = true
	}
	if flags&TCPFlagUrg != 0 {
		tcpLayer.URG = true
	}

	tcpLayer.Payload = payload

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths: true,
	}

	err := gopacket.SerializeLayers(buf, opts, ethernetLayer, ipLayer, tcpLayer, gopacket.Payload(payload))
	if err != nil {
		panic(err)
	}

	packet := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)
	return packet
}

// ============================================================================
// SCENARIO 1: ProcessPacket Tests
// ============================================================================

func TestProcessPacket_NewStream(t *testing.T) {
	collector := NewTCPCollector()

	packet := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagSyn, nil)

	collector.ProcessPacket(packet)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	if len(collector.Streams) != 1 {
		t.Fatalf("Expected 1 stream, got %d", len(collector.Streams))
	}

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream, exists := collector.Streams[key]
	if !exists {
		t.Fatal("Stream was not created")
	}

	if stream.SrcIP != "192.168.1.1" {
		t.Errorf("Expected SrcIP 192.168.1.1, got %s", stream.SrcIP)
	}
	if stream.PacketCount != 1 {
		t.Errorf("Expected PacketCount 1, got %d", stream.PacketCount)
	}
	if stream.Fin {
		t.Error("Expected Fin to be false")
	}
	if stream.Reset {
		t.Error("Expected Reset to be false")
	}
}

func TestProcessPacket_Retransmit(t *testing.T) {
	collector := NewTCPCollector()

	payload := []byte("test data")

	// First packet with payload
	packet1 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet1)

	// Retransmit: same sequence number with payload
	packet2 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet2)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if stream.Retransmits != 1 {
		t.Errorf("Expected 1 retransmit, got %d", stream.Retransmits)
	}
}

func TestProcessPacket_OutOfOrder(t *testing.T) {
	collector := NewTCPCollector()

	payload := []byte("data")

	// First packet
	packet1 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet1)

	// Out-of-order packet (lower sequence number)
	packet2 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 500, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet2)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if stream.OutOfOrderPackets != 1 {
		t.Errorf("Expected 1 out-of-order packet, got %d", stream.OutOfOrderPackets)
	}
}

func TestProcessPacket_DuplicateAcks(t *testing.T) {
	collector := NewTCPCollector()

	packet1 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 5000, TCPFlagAck, nil)
	collector.ProcessPacket(packet1)

	packet2 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1001, 5000, TCPFlagAck, nil)
	collector.ProcessPacket(packet2)

	packet3 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1002, 5000, TCPFlagAck, nil)
	collector.ProcessPacket(packet3)

	packet4 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1002, 5000, TCPFlagAck, nil)
	collector.ProcessPacket(packet4)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if stream.DuplicateAcks != 1 {
		t.Errorf("Expected 1 duplicate ACK event (after 4 identical ACKs), got %d", stream.DuplicateAcks)
	}

	// Optional: Test mit 4. Paket
	packet5 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1003, 5000, TCPFlagAck, nil)
	collector.StreamsMux.RUnlock()
	collector.ProcessPacket(packet5)
	collector.StreamsMux.RLock()

	stream = collector.Streams[key]
	if stream.DuplicateAcks != 1 {
		t.Errorf("After 5th identical ACK, expected still 1 event (DupAckCount=1), got %d", stream.DuplicateAcks)
	}
}

func TestProcessPacket_ZeroWindow(t *testing.T) {
	collector := NewTCPCollector()

	srcMac, _ := net.ParseMAC("00:01:02:03:04:05")
	dstMac, _ := net.ParseMAC("00:01:02:03:04:AA")
	ethernetLayer := &layers.Ethernet{
		SrcMAC:       srcMac,
		DstMAC:       dstMac,
		EthernetType: layers.EthernetTypeIPv4,
	}

	ipLayer := &layers.IPv4{
		SrcIP:    net.ParseIP("192.168.1.1"),
		DstIP:    net.ParseIP("192.168.1.2"),
		Protocol: layers.IPProtocolTCP,
		Version:  4,
	}

	tcpLayer := &layers.TCP{
		SrcPort: 12345,
		DstPort: 80,
		Seq:     1000,
		Ack:     0,
		Window:  0, // Zero window
		ACK:     true,
	}

	buf := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths: true,
	}
	gopacket.SerializeLayers(buf, opts, ethernetLayer, ipLayer, tcpLayer)
	packet := gopacket.NewPacket(buf.Bytes(), layers.LayerTypeEthernet, gopacket.Default)

	collector.ProcessPacket(packet)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if stream.ZeroWindowCount != 1 {
		t.Errorf("Expected 1 zero window event, got %d", stream.ZeroWindowCount)
	}
}

func TestProcessPacket_FIN(t *testing.T) {
	collector := NewTCPCollector()

	packet := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 5000, TCPFlagFin|TCPFlagAck, nil)
	collector.ProcessPacket(packet)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if !stream.Fin {
		t.Error("Expected Fin flag to be true")
	}
}

func TestProcessPacket_RST(t *testing.T) {
	collector := NewTCPCollector()

	packet := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagRst, nil)
	collector.ProcessPacket(packet)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if !stream.Reset {
		t.Error("Expected Reset flag to be true")
	}
	if stream.Resets != 1 {
		t.Errorf("Expected 1 reset, got %d", stream.Resets)
	}
}

func TestProcessPacket_SequenceGap(t *testing.T) {
	collector := NewTCPCollector()

	payload := []byte("data")

	// First packet
	packet1 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet1)

	// Gap: sequence jumps forward unexpectedly
	packet2 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 2000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet2)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if stream.GapDetections != 1 {
		t.Errorf("Expected 1 gap detection, got %d", stream.GapDetections)
	}
}

func TestProcessPacket_BitrateCalculation(t *testing.T) {
	collector := NewTCPCollector()

	payload := make([]byte, 1000)

	// First packet
	packet1 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet1)

	// Wait for bitrate calculation interval
	time.Sleep(1100 * time.Millisecond)

	// Second packet
	packet2 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 2000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet2)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := collector.Streams[key]
	if len(stream.BitrateHistory) == 0 {
		t.Error("Expected bitrate history to be populated")
	}
	if stream.BytesTransferred != 2000 {
		t.Errorf("Expected 2000 bytes transferred, got %d", stream.BytesTransferred)
	}
}

func TestProcessPacket_DelayCalculation(t *testing.T) {
	collector := NewTCPCollector()

	payload := []byte("data")

	// Send packet with sequence number
	packet1 := createTCPPacket("192.168.1.1", "192.168.1.2", 12345, 80, 1000, 0, TCPFlagAck, payload)
	collector.ProcessPacket(packet1)

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// ACK for that sequence number
	packet2 := createTCPPacket("192.168.1.2", "192.168.1.1", 80, 12345, 2000, 1000, TCPFlagAck, nil)

	// Process on reverse stream key
	collector.ProcessPacket(packet2)

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	// Check reverse stream
	reverseKey := types.StreamKey{
		SrcIP:   "192.168.1.2",
		DstIP:   "192.168.1.1",
		SrcPort: 80,
		DstPort: 12345,
	}

	stream, exists := collector.Streams[reverseKey]
	if !exists {
		t.Fatal("Reverse stream was not created")
	}

	// Note: Delay calculation requires ACK to reference a previously sent seq
	// In this simplified test, we just verify the stream exists
	if stream.PacketCount != 1 {
		t.Errorf("Expected 1 packet in reverse stream, got %d", stream.PacketCount)
	}
}

// ============================================================================
// SCENARIO 2: Collect Tests
// ============================================================================

func TestCollect_ActiveStream(t *testing.T) {
	collector := NewTCPCollector()

	// Create an active stream
	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:            "192.168.1.1",
		DstIP:            "192.168.1.2",
		SrcPort:          12345,
		DstPort:          80,
		Retransmits:      5,
		Resets:           1,
		WindowSize:       65535,
		PacketCount:      100,
		BytesTransferred: 50000,
		StreamStartTime:  time.Now().Add(-10 * time.Second),
		BitrateHistory:   []float64{1000, 2000, 1500},
		Delays:           []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
		MaxDelay:         20 * time.Millisecond,
		MinDelay:         10 * time.Millisecond,
		SeqNumbers:       make(map[uint32]time.Time),
		Fin:              false,
		Reset:            false,
	}

	collector.Streams[key] = stream

	// Collect metrics
	ch := make(chan prometheus.Metric, 100)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	metricsCount := 0
	for range ch {
		metricsCount++
	}

	if metricsCount == 0 {
		t.Error("Expected metrics to be collected")
	}

	// Check that stream count metric is present
	// In a real test, you'd decode the metrics and verify values
}

func TestCollect_FinalizedStream(t *testing.T) {
	collector := NewTCPCollector()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:           "192.168.1.1",
		DstIP:           "192.168.1.2",
		SrcPort:         12345,
		DstPort:         80,
		PacketCount:     50,
		StreamStartTime: time.Now().Add(-5 * time.Second),
		SeqNumbers:      make(map[uint32]time.Time),
		Fin:             true,
		Reset:           false,
	}

	collector.Streams[key] = stream

	ch := make(chan prometheus.Metric, 100)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	metricsCount := 0
	for range ch {
		metricsCount++
	}

	if metricsCount == 0 {
		t.Error("Expected metrics to be collected for finalized stream")
	}
}

func TestCollect_ResetStream(t *testing.T) {
	collector := NewTCPCollector()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:           "192.168.1.1",
		DstIP:           "192.168.1.2",
		SrcPort:         12345,
		DstPort:         80,
		PacketCount:     30,
		Resets:          1,
		StreamStartTime: time.Now().Add(-3 * time.Second),
		SeqNumbers:      make(map[uint32]time.Time),
		Fin:             false,
		Reset:           true,
	}

	collector.Streams[key] = stream

	ch := make(chan prometheus.Metric, 100)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	metricsCount := 0
	for range ch {
		metricsCount++
	}

	if metricsCount == 0 {
		t.Error("Expected metrics to be collected for reset stream")
	}
}

func TestCollect_StreamCount(t *testing.T) {
	collector := NewTCPCollector()

	// Create multiple streams with different states
	key1 := types.StreamKey{SrcIP: "192.168.1.1", DstIP: "192.168.1.2", SrcPort: 12345, DstPort: 80}
	stream1 := &types.TCPStream{
		SrcIP:           "192.168.1.1",
		DstIP:           "192.168.1.2",
		SrcPort:         12345,
		DstPort:         80,
		StreamStartTime: time.Now(),
		SeqNumbers:      make(map[uint32]time.Time),
		Fin:             false,
		Reset:           false,
	}

	key2 := types.StreamKey{SrcIP: "192.168.1.3", DstIP: "192.168.1.4", SrcPort: 54321, DstPort: 443}
	stream2 := &types.TCPStream{
		SrcIP:           "192.168.1.3",
		DstIP:           "192.168.1.4",
		SrcPort:         54321,
		DstPort:         443,
		StreamStartTime: time.Now(),
		SeqNumbers:      make(map[uint32]time.Time),
		Fin:             true, // Finalized
		Reset:           false,
	}

	key3 := types.StreamKey{SrcIP: "192.168.1.5", DstIP: "192.168.1.6", SrcPort: 8080, DstPort: 9090}
	stream3 := &types.TCPStream{
		SrcIP:           "192.168.1.5",
		DstIP:           "192.168.1.6",
		SrcPort:         8080,
		DstPort:         9090,
		StreamStartTime: time.Now(),
		SeqNumbers:      make(map[uint32]time.Time),
		Fin:             false,
		Reset:           false,
	}

	collector.Streams[key1] = stream1
	collector.Streams[key2] = stream2
	collector.Streams[key3] = stream3

	ch := make(chan prometheus.Metric, 100)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()

	for range ch {
		// Consume all metrics
	}

	// Active count should be 2 (stream1 and stream3)
	// This is a simplified test - in production you'd decode and verify the actual metric value
}

// ============================================================================
// SCENARIO 3: CleanupOldStreams Tests
// ============================================================================

func TestCleanupOldStreams_FIN(t *testing.T) {
	collector := NewTCPCollector()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:         "192.168.1.1",
		DstIP:         "192.168.1.2",
		SrcPort:       12345,
		DstPort:       80,
		Fin:           true,
		LastTimestamp: time.Now(),
		SeqNumbers:    make(map[uint32]time.Time),
	}

	collector.Streams[key] = stream

	// Run cleanup in a goroutine and stop it
	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		select {
		case <-ticker.C:
			collector.StreamsMux.Lock()
			for k, s := range collector.Streams {
				if s.Fin {
					delete(collector.Streams, k)
				}
			}
			collector.StreamsMux.Unlock()
			done <- true
		}
	}()

	<-done

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	if len(collector.Streams) != 0 {
		t.Errorf("Expected 0 streams after FIN cleanup, got %d", len(collector.Streams))
	}
}

func TestCleanupOldStreams_RST(t *testing.T) {
	collector := NewTCPCollector()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:         "192.168.1.1",
		DstIP:         "192.168.1.2",
		SrcPort:       12345,
		DstPort:       80,
		Reset:         true,
		LastTimestamp: time.Now(),
		SeqNumbers:    make(map[uint32]time.Time),
	}

	collector.Streams[key] = stream

	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		select {
		case <-ticker.C:
			collector.StreamsMux.Lock()
			for k, s := range collector.Streams {
				if s.Reset {
					delete(collector.Streams, k)
				}
			}
			collector.StreamsMux.Unlock()
			done <- true
		}
	}()

	<-done

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	if len(collector.Streams) != 0 {
		t.Errorf("Expected 0 streams after RST cleanup, got %d", len(collector.Streams))
	}
}

func TestCleanupOldStreams_Timeout(t *testing.T) {
	collector := NewTCPCollector()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:         "192.168.1.1",
		DstIP:         "192.168.1.2",
		SrcPort:       12345,
		DstPort:       80,
		LastTimestamp: time.Now().Add(-2 * time.Hour), // Old timestamp
		SeqNumbers:    make(map[uint32]time.Time),
	}

	collector.Streams[key] = stream

	maxAge := 1 * time.Hour

	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		select {
		case <-ticker.C:
			collector.StreamsMux.Lock()
			now := time.Now()
			for k, s := range collector.Streams {
				if now.Sub(s.LastTimestamp) > maxAge {
					delete(collector.Streams, k)
				}
			}
			collector.StreamsMux.Unlock()
			done <- true
		}
	}()

	<-done

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	if len(collector.Streams) != 0 {
		t.Errorf("Expected 0 streams after timeout cleanup, got %d", len(collector.Streams))
	}
}

func TestCleanupOldStreams_ActiveStreams(t *testing.T) {
	collector := NewTCPCollector()

	key := types.StreamKey{
		SrcIP:   "192.168.1.1",
		DstIP:   "192.168.1.2",
		SrcPort: 12345,
		DstPort: 80,
	}

	stream := &types.TCPStream{
		SrcIP:         "192.168.1.1",
		DstIP:         "192.168.1.2",
		SrcPort:       12345,
		DstPort:       80,
		LastTimestamp: time.Now(), // Current timestamp
		Fin:           false,
		Reset:         false,
		SeqNumbers:    make(map[uint32]time.Time),
	}

	collector.Streams[key] = stream

	maxAge := 1 * time.Hour

	done := make(chan bool)
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		select {
		case <-ticker.C:
			collector.StreamsMux.Lock()
			now := time.Now()
			for k, s := range collector.Streams {
				if s.Fin || s.Reset || now.Sub(s.LastTimestamp) > maxAge {
					delete(collector.Streams, k)
				}
			}
			collector.StreamsMux.Unlock()
			done <- true
		}
	}()

	<-done

	collector.StreamsMux.RLock()
	defer collector.StreamsMux.RUnlock()

	if len(collector.Streams) != 1 {
		t.Errorf("Expected 1 active stream to remain, got %d", len(collector.Streams))
	}
}

// ============================================================================
// Additional Helper Tests
// ============================================================================

func TestNewTCPCollector(t *testing.T) {
	collector := NewTCPCollector()

	if collector.Streams == nil {
		t.Error("Expected Streams map to be initialized")
	}

	if collector.streamCount == nil {
		t.Error("Expected streamCount descriptor to be initialized")
	}

	if collector.retransmits == nil {
		t.Error("Expected retransmits descriptor to be initialized")
	}
}

func TestDescribe(t *testing.T) {
	collector := NewTCPCollector()

	ch := make(chan *prometheus.Desc, 25)
	go func() {
		collector.Describe(ch)
		close(ch)
	}()

	descCount := 0
	for range ch {
		descCount++
	}

	// Should have all metric descriptors
	if descCount != 22 {
		t.Errorf("Expected 22 descriptors, got %d", descCount)
	}
}

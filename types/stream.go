package types

import (
	"fmt"
	"time"
)

type TCPStream struct {
	SrcIP          string
	DstIP          string
	SrcPort        uint16
	DstPort        uint16
	Retransmits    int
	Resets         int
	WindowSize     uint16
	LastSeq        uint32
	LastAck        uint32
	LastTimestamp  time.Time
	Delays         []time.Duration
	PacketCount    int
	SeqNumbers     map[uint32]time.Time
	Fin            bool
	FinTimestamp   time.Time
	Reset          bool
	ResetTimestamp time.Time

	BytesTransferred  uint64
	LastByteCount     uint64
	BitrateHistory    []float64
	LastBitrateCheck  time.Time
	PacketSizes       []int
	InterArrivalTimes []time.Duration
	LastPacketTime    time.Time
	OutOfOrderPackets int
	DuplicateAcks     int
	LastDupAck        uint32
	DupAckCount       int
	ZeroWindowCount   int
	StreamStartTime   time.Time
	GapDetections     int
	MaxDelay          time.Duration
	MinDelay          time.Duration
}

type StreamKey struct {
	SrcIP   string
	DstIP   string
	SrcPort uint16
	DstPort uint16
}

func (sk StreamKey) String() string {
	return fmt.Sprintf("%s:%d->%s:%d", sk.SrcIP, sk.SrcPort, sk.DstIP, sk.DstPort)
}

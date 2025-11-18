package bpf

import (
	"fmt"
	"github.com/google/gopacket/pcap"
	"golang.org/x/net/bpf"
)

func CreateBPFFilter(bpfFilter string) ([]bpf.RawInstruction, error) {
	// snaplen: 65535 (Maximum), linkType: Ethernet
	instructions, err := pcap.CompileBPFFilter(1 /* DLT_EN10MB = Ethernet */, 65535, bpfFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to compile BPF filter '%s': %w", bpfFilter, err)
	}

	rawInstructions := make([]bpf.RawInstruction, len(instructions))
	for i, inst := range instructions {
		rawInstructions[i] = bpf.RawInstruction{
			Op: inst.Code,
			Jt: inst.Jt,
			Jf: inst.Jf,
			K:  inst.K,
		}
	}

	return rawInstructions, nil
}

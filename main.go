package main

import (
	"flag"
	"fmt"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"log/slog"
	"net/http"
	"os"
	"tcp-stream-exporter/bpf"
	"tcp-stream-exporter/prom"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/afpacket"
	"github.com/google/gopacket/layers"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func initLogger(debug bool) *slog.Logger {
	logLevel := slog.LevelInfo
	if debug {
		logLevel = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{
		Level: logLevel,
	}
	handler := slog.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

func main() {
	var (
		iface        = flag.String("interface", "eth0", "Network interface to capture on")
		port         = flag.String("port", ":9100", "Port for Prometheus metrics")
		bpfFilter    = flag.String("packet-filter", "tcp", "tcpdump-compatible packet filter string")
		streamAge    = flag.Duration("stream-age", 5*time.Minute, "Max age for inactive streams")
		finalizedAge = flag.Duration("finalized-age", 1*time.Minute, "Max age for finalized (FIN/RST) streams")
		frameSize    = flag.Int("framesize", 2048, "Frame size for AF_PACKET")
		blockSize    = flag.Int("blocksize", 2048*128, "Block size for AF_PACKET (should be multiple of framesize)")
		numBlocks    = flag.Int("numblocks", 128, "Number of blocks for AF_PACKET ring buffer")
		debug        = flag.Bool("debug", false, "Enable debug logging")
		goMetrics    = flag.Bool("go-metrics", false, "Enable Go Metrics")
		procMetrics  = flag.Bool("process-metrics", false, "Enable process metrics")
	)
	flag.Parse()

	logger := initLogger(*debug)
	slog.SetDefault(logger)

	logger.Info("Starting TCPv4 Statistics Exporter")

	logger.Info("AF_PACKET config", "interface", *iface, "framesize", *frameSize, "blocksize", *blockSize, "numblocks", *numBlocks, "port_filter", *bpfFilter)

	tpacket, err := afpacket.NewTPacket(
		afpacket.OptInterface(*iface),
		afpacket.OptFrameSize(*frameSize),
		afpacket.OptBlockSize(*blockSize),
		afpacket.OptNumBlocks(*numBlocks),
		afpacket.OptPollTimeout(100*time.Millisecond),
	)
	if err != nil {
		logger.Error("Error creating AF_PACKET receiver", "error", err)
		os.Exit(1)
	}
	defer tpacket.Close()

	filter, err := bpf.CreateBPFFilter(*bpfFilter)
	if err != nil {
		logger.Error("Error creating BPF filter", "error", err)
		os.Exit(1)
	}
	if err := tpacket.SetBPF(filter); err != nil {
		logger.Error("Error setting BPF filter", "error", err)
		os.Exit(1)
	}
	logger.Info("BPF filter applied successfully")

	collector := prom.NewTCPCollector()
	prometheus.MustRegister(collector)

	if !*goMetrics {
		prometheus.Unregister(collectors.NewGoCollector())
	}

	if !*procMetrics {
		prometheus.Unregister(collectors.NewProcessCollector(
			collectors.ProcessCollectorOpts{},
		))
	}

	go collector.CleanupOldStreams(*streamAge, *finalizedAge)

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			stats, statsV3, err := tpacket.SocketStats()
			if err != nil {
				logger.Error("Error getting stats", "error", err)
				continue
			}

			collector.StreamsMux.RLock()
			streamCount := len(collector.Streams)
			collector.StreamsMux.RUnlock()

			if statsV3.Packets() > 0 {
				dropRate := float64(statsV3.Drops()) / float64(statsV3.Packets()) * 100
				logger.Info("Stats", "streams", streamCount, "packets", statsV3.Packets(), "drops", statsV3.Drops(), "drop_rate", dropRate, "queue_freeze", statsV3.QueueFreezes())
			} else if stats.Packets() > 0 {
				dropRate := float64(stats.Drops()) / float64(stats.Packets()) * 100
				logger.Info("Stats", "streams", streamCount, "packets", stats.Packets(), "drops", stats.Drops(), "drop_rate", dropRate)
			}
		}
	}()

	go func() {
		logger.Info("Starting packet capture...")
		packetSource := gopacket.NewPacketSource(tpacket, layers.LayerTypeEthernet)
		packetSource.DecodeOptions.Lazy = true
		packetSource.DecodeOptions.NoCopy = true

		for packet := range packetSource.Packets() {
			collector.ProcessPacket(packet)
		}
	}()

	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		stats, statsV3, _ := tpacket.SocketStats()

		html := `<html>
			<head><title>TCP Stats Exporter (AF_PACKET)</title></head>
			<body>
			<h1>TCP Statistics Exporter (AF_PACKET)</h1>
			<p><a href="/metrics">Metrics</a></p>`

		if statsV3.Packets() > 0 {
			html += fmt.Sprintf(`
			<h2>Capture Statistics:</h2>
			<ul>
				<li>Packets: %d</li>
				<li>Drops: %d</li>
				<li>Queue Freezes: %d</li>
			</ul>`, statsV3.Packets(), statsV3.Drops(), statsV3.QueueFreezes())
		} else if stats.Packets() > 0 {
			html += fmt.Sprintf(`
			<h2>Capture Statistics:</h2>
			<ul>
				<li>Packets: %d</li>
				<li>Drops: %d</li>
			</ul>`, stats.Packets(), stats.Drops())
		}

		html += `</body></html>`
		_, err := w.Write([]byte(html))
		if err != nil {
			logger.Error(fmt.Sprintf("Error writing HTTP response for / to client %s", r.RemoteAddr), "error", err)
		}
	})

	logger.Info(fmt.Sprintf("Starting HTTP server on %s", *port))
	logger.Error(fmt.Sprintf("%s", http.ListenAndServe(*port, nil)))
}

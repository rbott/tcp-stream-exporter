# TCP Stream Exporter

This Prometheus exporter provides metrics on active TCP streams. A stream is defined as TCP packets flowing between
the same source/destination IP/port combination. That means a regular TCP connection will be observed as **two**
separate TCP streams by the exporter.

It requires `libpcap` to be present on the system, but only to create the BPF filter from a tcpdump-compatible filter
string (`-packet-filter` Parameter). `tcp-stream-exporter` uses `AF_PACKET` (with zero-copy ring buffer) for packet
gathering which is _much_ faster than `libpcap` and less complicated to use than XDP + eBPF (from a Go perspective).

Some comparison stats (according to claude.ai):
- `libpcap`: 1-2 Million pps (CPU 80-90%)
- `AF_PACKET` (w/ zero-copy ring buffer): 4-6 Million pps (CPU 50-60%)
- `XDP + eBPF`: 10-24 Million pps (CPU 30-40%)

The exporter currently only observes IPV4 TCP streams.

## Performance / Requirements

I did some local benchmarking with the following setup:

- simple Golang webserver listening on `lo` which responds with random-length answers
- two instances of `wrk -t1000 -c1500 -d10m http://127.0.0.1:8080/` (1000 Threads, 1500 connections, 10 minute duration)
- tcp-stream-exporter commandline: `./tcp-stream-exporter -interface lo -packet-filter "tcp and port 8080" -stream-age 30s -finalized-age 30s -numblocks 1024`

`tcp-stream-exporter` observes 4000 parallel streams as expected and reports 0 dropped packets. Memory usage is around
700-850MB, CPU usage is 90-140% (e.g. slightly more than one core on an AMD Ryzen 7 PRO 6850U), traffic level ~2.7GBit/s.
Canceling and restarting `wrk` processes quadrupels the stream count for a while (until timed out streams are cleaned up).
**However**, the cleanup process seems to be slow/blocking and leads to a large number of dropped packets in the exporter
**if** many stale TCP connections need to be removed (e.g. 800-1000). There seems to be room for improvement here.

## Usage
```text
  -blocksize int
        Block size for AF_PACKET (should be multiple of framesize) (default 262144)
  -debug
        Enable debug logging
  -finalized-age duration
        Max age for finalized (FIN/RST) streams (default 1m0s)
  -framesize int
        Frame size for AF_PACKET (default 2048)
  -go-metrics
        Enable Go Metrics
  -interface string
        Network interface to capture on (default "eth0")
  -numblocks int
        Number of blocks for AF_PACKET ring buffer (default 128)
  -packet-filter string
        tcpdump-compatible packet filter string (default "tcp")
  -port string
        Port for Prometheus metrics (default ":9100")
  -process-metrics
        Enable process metrics
  -stream-age duration
        Max age for inactive streams (default 5m0s)
```

## Examples

```shell
# observe any TCP packet on eth0
./tcp-stream-exporter
```

```shell
# observe TCP packets with src/dst port 80 on eth1
./tcp-stream-exporter -interface eth1 -packet-filter "tcp and port 80"
```

```shell
# observe TCP packets with src/dst port 80 and forget inactive streams after 60s
./tcp-stream-exporter -packet-filter "tcp and port 80" -stream-age 60s
```

```shell
# observe TCP packets with src/dst port 80 and raise ring buffer to 512 (if many drops are reported)
./tcp-stream-exporter -packet-filter "tcp and port 80" -numblocks 512
```

## Packet filter

The exporter supports the standard [tcpdump filter language](https://www.tcpdump.org/manpages/pcap-filter.7.html). There
are a few things to keep in mind:

- the exporter will examine any package (up to the TCP header) it receives so your filter should be as strict as possible
- the default filter is `tcp` (which will capture any TCP packet), you should extend from here (and never drop the `tcp`)
- overly complex filters might raise CPU usage
- you can easily test your filters in advance with `tcpdump`

## Stream expiration

`tcp-stream-exporter` has two timeout values:

### `-finalized-age`
If the exporter observes a FIN or RST flag on a stream, it considers this stream to be finalized and removes it from
the overall count of active streams. However, it will still export information on the stream (with the `state=finalized`
label). Finalized streams will be removed roughly after the timeout given in `-finalized-age` (defaults to 60 seconds).
This increases the chance of very short-lived streams to be picked up by the scraper.

### `-max-age`
This defines the timeout after which `tcp-stream-exporter` considers a stream dead when no new packets have been received.
This might happen if the remote end of an established connection suddenly vanishes or if a TCP connection is never fully
established because there is no response to SYN packets.

## Odd number of active TCP streams

Since `tcp-stream-exporter` does currently not have a concept of an actual TCP connection (e.g. two streams belonging to
each other), it might sometimes report an odd number of streams. This could be for multiple reasons:
- it observes SYN packets which are never replied to
- it misses (for whatever reason) the packet with the RST/FIN flag in one direction of the connection

In both cases the `-max-age` stream timeout will eventually remove the stream.

## Metrics exported

```
tcp_stream_count: Number of active TCP streams
tcp_stream_health_score: Stream health score (0-100, 100=perfect)
tcp_stream_uptime_seconds: Stream uptime in seconds

tcp_avg_delay_seconds: Average delay per stream
tcp_bitrate_bps: Current bitrate in bits per second
tcp_bitrate_variance: Variance in bitrate (coefficient of variation)
tcp_bytes_transferred_total: Total bytes transferred
tcp_duplicate_acks_total: Number of duplicate ACKs
tcp_interarrival_variance_seconds: Variance in packet inter-arrival times
tcp_jitter_seconds: Jitter (delay variance) per stream
tcp_max_delay_seconds: Maximum delay observed
tcp_min_delay_seconds: Minimum delay observed
tcp_out_of_order_packets_total: Number of out-of-order packets
tcp_packet_rate_pps: Packet rate in packets per second
tcp_packet_size_variance: Variance in packet sizes (coefficient of variation)
tcp_resets_total: Number of TCP resets per stream
tcp_retransmits_total: Number of TCP retransmissions per stream
tcp_sequence_gaps_total: Number of gaps detected in sequence numbers
tcp_window_size_bytes: TCP window size per stream
tcp_zero_window_events_total: Number of zero window events

capture_dropped_packets_total: Number of packets dropped by kernel
capture_packets_processed_total: Number of packets processed
capture_queue_freezes_total: Number of queue freezes (only AF_PACKET v3)
```

You can additionally enable the built-in process/runtime metrics with the flags `-go-metrics` and `-process-metrics`.

## Labels

Most metrics will include the following labels:

- `dst_ip`
- `dst_port`
- `src_ip`
- `src_port`
- `state` with values `active` or `finalized`

## SystemD

The recommended way would be running the exporter as a non-privileged user (but with extra capabilities to allow packet
sniffing). 
```shell
useradd -M -r -s /usr/sbin/nologin -d /var/lib/tcp-stream-exporter tcp-stream-exporter
```

Place the following file in `/etc/default/tcp-stream-exporter` and add any commandline flags you need:
```text
STREAM_EXPORTER_PARAMETERS=""
```

Place the following file in `/etc/systemd/system/tcp-stream-exporter.service`:
```
[Unit]
Description=TCP Stream Exporter
After=network.target

[Service]
Type=simple
User=tcp-stream-exporter
ExecStart=/usr/bin/tcp-stream-exporter $STREAM_EXPORTER_PARAMETERS
EnvironmentFile=/etc/default/tcp-stream-exporter
Restart=on-failure
RestartSec=5s
AmbientCapabilities=CAP_NET_RAW CAP_NET_ADMIN
CapabilityBoundingSet=CAP_NET_RAW CAP_NET_ADMIN
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadOnlyPaths=/
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallFilter=@system-service
SystemCallFilter=~@privileged @resources
SystemCallArchitectures=native

[Install]
WantedBy=multi-user.target
```

Run the following commands:
```shell
systemctl daemon-reload
systemctl enable tcp-stream-exporter
systemctl start tcp-stream-exporter

systemctl status tcp-stream-exporter
```
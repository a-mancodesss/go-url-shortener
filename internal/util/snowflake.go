package util

import (
	"errors"
	"sync"
	"time"
)

// Snowflake is a distributed unique ID generator inspired by Twitter Snowflake.
// Structure (64 bits):
//   1 bit  - unused (sign)
//   41 bits - milliseconds since custom epoch
//   10 bits - node/machine ID (0-1023)
//   12 bits - per-millisecond sequence (0-4095)
//
// This gives:
//   - ~69 years of IDs from epoch
//   - 1024 nodes
//   - 4096 IDs per ms per node = ~4M IDs/sec/node
//
// Why Snowflake vs auto-increment?
//   + Decentralized, no DB bottleneck, sortable by time
//   + No single point of failure (DB sequence)
//   - Requires node ID coordination (env var / k8s statefulset)
//   - Clock drift must be handled
//
// Alternative considered: Postgres SERIAL + Base62 -> simpler but single-writer bottleneck,
// exposes count, harder to shard. UUIDv4 -> 128-bit, not URL-friendly, random (no ordering).
// Hash (MD5+Base62) -> collision risk, needs dedup check.
//
// Production note: nodeID should be assigned via env POD_ID / StatefulSet ordinal
// or via Redis/ZK coordination at startup.

const (
	epoch          int64 = 1704067200000 // 2024-01-01 00:00:00 UTC in ms
	nodeBits       uint8 = 10
	sequenceBits   uint8 = 12
	maxNodeID      int64 = -1 ^ (-1 << nodeBits)     // 1023
	maxSequence    int64 = -1 ^ (-1 << sequenceBits) // 4095
	nodeShift      uint8 = sequenceBits
	timestampShift uint8 = sequenceBits + nodeBits
)

type Snowflake struct {
	mu       sync.Mutex
	nodeID   int64
	sequence int64
	lastTS   int64
}

// NewSnowflake creates a generator for the given nodeID [0,1023].
func NewSnowflake(nodeID int64) (*Snowflake, error) {
	if nodeID < 0 || nodeID > maxNodeID {
		return nil, errors.New("nodeID must be between 0 and 1023")
	}
	return &Snowflake{nodeID: nodeID}, nil
}

// NextID generates the next unique ID (thread-safe).
func (s *Snowflake) NextID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := max(time.Now().UnixMilli(),
		// Clock moved backwards — wait until catch-up or use last timestamp
		// In production, you may want to log/alert and wait.
		s.lastTS)

	if ts == s.lastTS {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// Sequence overflow in same ms — spin until next ms
			for ts <= s.lastTS {
				ts = time.Now().UnixMilli()
			}
		}
	} else {
		s.sequence = 0
	}

	s.lastTS = ts

	id := ((ts - epoch) << timestampShift) | (s.nodeID << nodeShift) | s.sequence
	return id
}

// NextCode generates a Base62-encoded short code from Snowflake ID.
func (s *Snowflake) NextCode() string {
	return EncodeBase62(s.NextID())
}

package stats

import (
	"hash/fnv"
	"math"
	"math/bits"
)

// HyperLogLog distinct-count sketch.
//
// Implementation is plain HLL (Flajolet, Fusy, Gandouet, Meunier 2007)
// with the FlPM07 small-range correction. This is sufficient for our
// scale (per-column NDV up to ~10M, with target relative error ~1.6%).
// We do not implement the HLL++ sparse representation: the dense
// representation is 12 KB per HLL and we always finalise to dense for
// the sidecar, so sparse/dense duality isn't worth the complexity here.
//
// Plan §1.3 budget: ~12 KB per column.

const (
	// hllPrecision = 14 → m = 16384 registers, std error ≈ 1.04/√m ≈ 0.81%
	// (relative). 14 was chosen to land on the plan's 12 KB/column figure
	// (16384 * 6 bits packed → 12 KB; we use a byte per register here for
	// simplicity, doubling memory in flight but keeping serialisation
	// straightforward — disk emits packed; see save()/load()).
	hllPrecision  = 14
	hllRegisters  = 1 << hllPrecision // 16384
	hllPrefixBits = 64 - hllPrecision
)

// HLL is a HyperLogLog sketch over uint64 hashes.
type HLL struct {
	registers [hllRegisters]uint8
}

// NewHLL returns an empty HLL sketch.
func NewHLL() *HLL { return &HLL{} }

// AddHashed updates the sketch with a precomputed 64-bit hash. Use
// AddBytes / AddUint64 for the common cases.
func (h *HLL) AddHashed(hash uint64) {
	idx := hash >> hllPrefixBits // top precision bits
	w := (hash << hllPrecision) | (1 << (hllPrecision - 1))
	rho := uint8(bits.LeadingZeros64(w)) + 1
	if rho > h.registers[idx] {
		h.registers[idx] = rho
	}
}

// AddUint64 hashes v and updates the sketch.
func (h *HLL) AddUint64(v uint64) {
	h.AddHashed(mix64(v))
}

// AddBytes hashes b (FNV-1a 64, finalised through splitmix64) and
// updates the sketch. Used for string-typed columns where the value
// is the interned string.
//
// The splitmix64 finaliser is necessary because FNV-1a 64 has weak
// avalanche in the upper bits for short inputs — and HLL's register
// selection uses the top `hllPrecision` bits, which would land
// almost-all collisions on a small set of registers and underestimate
// distinct counts by an order of magnitude (verified empirically on
// "v0".."v49999").
func (h *HLL) AddBytes(b []byte) {
	hh := fnv.New64a()
	_, _ = hh.Write(b)
	h.AddHashed(mix64(hh.Sum64()))
}

// Estimate returns the cardinality estimate.
func (h *HLL) Estimate() int64 {
	const m = float64(hllRegisters)
	alpha := 0.7213 / (1.0 + 1.079/m) // standard correction for m≥128

	var sum float64
	zeros := 0
	for _, r := range h.registers {
		sum += 1.0 / float64(uint64(1)<<r)
		if r == 0 {
			zeros++
		}
	}
	est := alpha * m * m / sum

	// Small-range correction (linear counting) per FlPM07.
	if est <= 2.5*m && zeros != 0 {
		return int64(math.Round(m * math.Log(m/float64(zeros))))
	}
	// Large-range correction is unnecessary for our register width.
	return int64(math.Round(est))
}

// Merge folds other into h (register-wise max). Used in JoinStats
// distinct-match estimation: |A ∩ B| ≈ |A| + |B| − |A ∪ B|, with
// |A ∪ B| computed by merging the sketches.
func (h *HLL) Merge(other *HLL) {
	if other == nil {
		return
	}
	for i, r := range other.registers {
		if r > h.registers[i] {
			h.registers[i] = r
		}
	}
}

// Clone returns a deep copy.
func (h *HLL) Clone() *HLL {
	if h == nil {
		return nil
	}
	c := &HLL{}
	c.registers = h.registers
	return c
}

// IntersectEstimate returns the inclusion-exclusion estimate of |A ∩ B|.
// Note this can be negative when |A ∪ B| ≈ |A| + |B|; we clamp to zero.
func IntersectEstimate(a, b *HLL) int64 {
	if a == nil || b == nil {
		return 0
	}
	u := a.Clone()
	u.Merge(b)
	r := a.Estimate() + b.Estimate() - u.Estimate()
	if r < 0 {
		return 0
	}
	return r
}

// mix64 is a fast integer hash (splitmix64 finaliser) used for uint64
// inputs so that low-entropy ids (1, 2, 3, ...) get spread across the
// hash space before HLL register selection.
func mix64(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

package audio

// Handle identifies one playing voice. The zero value is never a live voice,
// so a Handle can be stored in components and checked against NoHandle.
//
// Handles are generation-encoded (voice-slot index in the low 32 bits,
// slot generation in the high 32). When a voice ends its slot's generation
// bumps, so control calls through a stale handle — Stop, SetVolume, … —
// are safe no-ops rather than hijacking whatever reused the slot.
type Handle uint64

// NoHandle is the invalid Handle. Play returns it for unknown refs and when
// every voice slot is protected.
const NoHandle Handle = 0

func makeHandle(idx int, gen uint32) Handle {
	return Handle(uint64(gen)<<32 | uint64(uint32(idx)))
}

func (h Handle) index() int        { return int(uint32(h)) }
func (h Handle) generation() uint32 { return uint32(h >> 32) }

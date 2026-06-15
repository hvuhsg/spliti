package game

// Collision layers: each name is one bit in collision Layer/Mask fields.
// The editor's Layers panel edits this block in place — rename or append
// freely, but do not remove or reorder entries (the bit positions are baked
// into compiled code and saved scenes).
//
//spliti:layers
const (
	LayerDefault uint32 = 1 << iota
	LayerPlayer
	LayerEnemy
)

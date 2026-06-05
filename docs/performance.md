# Performance — Tasks to Improve

A list of actionable tasks to reduce frame cost in the render loop. The bottleneck is **draw-call count and GPU work**, not the cgo boundary, so all tasks target the former.

- [ ] **Batch transparent objects by material.** The transparent pass (`plugin/render3d/render.go`) draws each item individually at 5 cgo/GPU calls each. Apply the same (mesh, material) batching the opaque path uses.
- [ ] **Consolidate overlay 2D panels.** Each panel costs 3 calls. Merge panels using offset arrays / a single batched draw.
- [ ] **Cache material bind groups.** Skip redundant `SetBindGroup(1, …)` when the same material repeats across consecutive draws.
- [ ] **Reduce overall draw calls.** Extend material batching everywhere geometry is drawn per-object; keep crossings proportional to draw calls, not geometry.
- [ ] **Consider indirect rendering for very high draw counts.** Advanced; requires compute support. Only pursue if batching is exhausted and draw counts still dominate the frame.
- [ ] **Watch Go↔C pointer passing.** Keep uploads batched (instance data in a single `WriteBuffer`) so per-frame heap allocations stay bounded.

When profiling a slow frame, confirm the cause is GPU work or draw-call count before changing anything else.

package srcmap

// RuntimeBridge combines static index data with runtime EVM access events.
type RuntimeBridge struct {
	index *Index
}

// NewRuntimeBridge constructs a runtime annotation bridge.
func NewRuntimeBridge(index *Index) *RuntimeBridge {
	return &RuntimeBridge{index: index}
}

// AnnotateMemoryAccess explains a runtime memory access using source-map context.
func (b *RuntimeBridge) AnnotateMemoryAccess(access MemoryAccess) MemoryAnnotation {
	annotation := MemoryAnnotation{
		Access:     access,
		Region:     classifyMemoryRegion(access.Offset, access.Size),
		Confidence: MemoryConfidenceLow,
		Reason:     "no matching instruction for PC",
	}
	if b == nil || b.index == nil {
		return annotation
	}
	instruction, ok := b.index.InstructionAtPC(access.PC)
	if !ok {
		return annotation
	}
	annotation.Instruction = instruction
	annotation.Node = instruction.AST
	annotation.Confidence = memoryConfidenceForRegion(annotation.Region)
	annotation.Reason = memoryReason(instruction, annotation.Region)
	return annotation
}

// AnnotateStorageAccess explains a runtime storage access using layout data.
func (b *RuntimeBridge) AnnotateStorageAccess(access StorageAccess) StorageAnnotation {
	annotation := StorageAnnotation{
		Access: access,
		Reason: "no matching declared storage root",
	}
	if b == nil || b.index == nil {
		return annotation
	}
	if instruction, ok := b.index.InstructionAtPC(access.PC); ok {
		annotation.Instruction = instruction
	}
	annotation.Variables = b.index.StorageBySlot(access.Slot, access.Scope)
	if len(annotation.Variables) > 0 {
		annotation.Reason = "matched declared storage root slot"
	}
	return annotation
}

func classifyMemoryRegion(offset, size uint64) MemoryRegion {
	if size == 0 {
		return MemoryRegion{Kind: MemoryRegionUnknown, Offset: offset, Size: size, Label: "empty access"}
	}
	end := offset + size
	if offset < 0x40 && end <= 0x40 {
		return MemoryRegion{Kind: MemoryRegionScratch, Offset: offset, Size: size, Label: "scratch space"}
	}
	if offset < 0x60 && end <= 0x60 {
		return MemoryRegion{Kind: MemoryRegionFreePtr, Offset: offset, Size: size, Label: "free memory pointer"}
	}
	if offset < 0x80 && end <= 0x80 {
		return MemoryRegion{Kind: MemoryRegionZeroSlot, Offset: offset, Size: size, Label: "zero slot"}
	}
	if offset >= 0x80 {
		return MemoryRegion{Kind: MemoryRegionHeap, Offset: offset, Size: size, Label: "heap or ABI buffer"}
	}
	return MemoryRegion{Kind: MemoryRegionTemporary, Offset: offset, Size: size, Label: "mixed reserved region"}
}

func memoryConfidenceForRegion(region MemoryRegion) MemoryConfidence {
	switch region.Kind {
	case MemoryRegionScratch, MemoryRegionFreePtr, MemoryRegionZeroSlot:
		return MemoryConfidenceHigh
	case MemoryRegionHeap:
		return MemoryConfidenceMedium
	default:
		return MemoryConfidenceLow
	}
}

func memoryReason(instruction *InstructionMapping, region MemoryRegion) string {
	if instruction == nil {
		return "no matching instruction for PC"
	}
	if instruction.AST == nil {
		return "matched instruction, but no AST node covers the source range"
	}
	if region.Kind == MemoryRegionHeap {
		return "matched instruction source range; heap mapping remains dynamic and best-effort"
	}
	return "matched instruction source range and reserved Solidity memory region"
}
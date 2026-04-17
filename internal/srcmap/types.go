package srcmap

import "fmt"

// BuildConfig selects a contract artifact from Solidity standard-json output.
type BuildConfig struct {
	SourceName   string
	ContractName string
	Runtime      bool
}

// SourceRange identifies a byte range inside a source file.
type SourceRange struct {
	SourceID int
	Start    int
	Length   int
}

// End returns the exclusive end of the source range.
func (r SourceRange) End() int {
	return r.Start + r.Length
}

// ContainsOffset reports whether the given byte offset is inside the range.
func (r SourceRange) ContainsOffset(offset int) bool {
	return offset >= r.Start && offset < r.End()
}

// Overlaps reports whether two source ranges intersect.
func (r SourceRange) Overlaps(other SourceRange) bool {
	if r.SourceID != other.SourceID {
		return false
	}
	return r.Start < other.End() && other.Start < r.End()
}

func (r SourceRange) String() string {
	return fmt.Sprintf("%d:%d:%d", r.Start, r.Length, r.SourceID)
}

// SourceFile describes a Solidity or generated source file.
type SourceFile struct {
	ID        int
	Name      string
	Content   string
	Generated bool
	Language  string
	AST       *ASTNode
}

// ASTNode is a compact generic representation of a Solidity AST node.
type ASTNode struct {
	ID       int
	NodeType string
	Name     string
	Src      SourceRange
	ParentID int
	Children []*ASTNode
	Raw      map[string]any
}

// JumpType mirrors Solidity source-map jump markers.
type JumpType string

const (
	JumpNone JumpType = "-"
	JumpInto JumpType = "i"
	JumpOut  JumpType = "o"
)

// InstructionMapping connects one instruction to source metadata.
type InstructionMapping struct {
	InstructionIndex int
	PC               uint64
	Opcode           byte
	PushBytes        int
	Source           SourceRange
	Jump             JumpType
	ModifierDepth    int
	AST              *ASTNode
}

// StorageLayout is the subset of Solidity storage layout used by the index.
type StorageLayout struct {
	Storage []StorageEntry          `json:"storage"`
	Types   map[string]StorageType  `json:"types"`
}

// StorageEntry represents one declared storage variable or member.
type StorageEntry struct {
	ASTID    int    `json:"astId"`
	Contract string `json:"contract"`
	Label    string `json:"label"`
	Offset   int    `json:"offset"`
	Slot     string `json:"slot"`
	Type     string `json:"type"`
}

// StorageType describes how a storage value is encoded.
type StorageType struct {
	Encoding      string         `json:"encoding"`
	Label         string         `json:"label"`
	NumberOfBytes string         `json:"numberOfBytes"`
	Base          string         `json:"base,omitempty"`
	Key           string         `json:"key,omitempty"`
	Value         string         `json:"value,omitempty"`
	Members       []StorageEntry `json:"members,omitempty"`
}

// StorageVariableMapping ties a source node to a storage slot root.
type StorageVariableMapping struct {
	Entry StorageEntry
	Type  StorageType
	AST   *ASTNode
	Root  SourceRange
	Scope StorageScope
}

// StorageScope selects persistent vs transient storage.
type StorageScope string

const (
	StorageScopePersistent StorageScope = "storage"
	StorageScopeTransient  StorageScope = "transient"
)

// MemoryRegionKind identifies broad Solidity memory regions.
type MemoryRegionKind string

const (
	MemoryRegionScratch   MemoryRegionKind = "scratch"
	MemoryRegionFreePtr   MemoryRegionKind = "free_memory_pointer"
	MemoryRegionZeroSlot  MemoryRegionKind = "zero_slot"
	MemoryRegionHeap      MemoryRegionKind = "heap"
	MemoryRegionTemporary MemoryRegionKind = "temporary"
	MemoryRegionUnknown   MemoryRegionKind = "unknown"
)

// MemoryConfidence declares how strong a memory annotation is.
type MemoryConfidence string

const (
	MemoryConfidenceHigh   MemoryConfidence = "high"
	MemoryConfidenceMedium MemoryConfidence = "medium"
	MemoryConfidenceLow    MemoryConfidence = "low"
)

// MemoryRegion describes a memory interval with a semantic label.
type MemoryRegion struct {
	Kind   MemoryRegionKind
	Offset uint64
	Size   uint64
	Label  string
}

// MemoryAccess describes a runtime memory read/write event.
type MemoryAccess struct {
	PC      uint64
	Opcode  byte
	Offset  uint64
	Size    uint64
	IsWrite bool
	Data    []byte
}

// StorageAccess describes a runtime storage read/write event.
type StorageAccess struct {
	PC      uint64
	Opcode  byte
	Slot    string
	IsWrite bool
	Value   string
	Scope   StorageScope
}

// MemoryAnnotation explains a runtime memory event in source terms.
type MemoryAnnotation struct {
	Access     MemoryAccess
	Instruction *InstructionMapping
	Node       *ASTNode
	Region     MemoryRegion
	Confidence MemoryConfidence
	Reason     string
}

// StorageAnnotation explains a runtime storage event in source terms.
type StorageAnnotation struct {
	Access      StorageAccess
	Instruction *InstructionMapping
	Variables   []StorageVariableMapping
	Reason      string
}

// Index is the main bidirectional lookup structure.
type Index struct {
	Build                  BuildConfig
	Sources                map[int]*SourceFile
	Instructions           []InstructionMapping
	InstructionByPC        map[uint64]*InstructionMapping
	NodesByID              map[int]*ASTNode
	NodesBySourceID        map[int][]*ASTNode
	StorageVariables       []StorageVariableMapping
	TransientVariables     []StorageVariableMapping
	PersistentLayout       StorageLayout
	TransientLayout        StorageLayout
	CreationBytecode       []byte
	RuntimeBytecode        []byte
	GeneratedSourceIDs     map[int]struct{}
}
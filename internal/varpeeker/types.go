package varpeeker

// Confidence describes how strong a peeked value is.
type Confidence string

const (
	ConfidenceHigh        Confidence = "high"
	ConfidenceMedium      Confidence = "medium"
	ConfidenceLow         Confidence = "low"
	ConfidenceUnavailable Confidence = "unavailable"
)

// VariableKind classifies where a variable lives logically.
type VariableKind string

const (
	KindParameter VariableKind = "parameter"
	KindReturn    VariableKind = "return"
	KindLocal     VariableKind = "local"
	KindStorage   VariableKind = "storage"
	KindTransient VariableKind = "transient"
	KindImmutable VariableKind = "immutable"
	KindConstant  VariableKind = "constant"
)

// LocationKind tells the consumer where the value was read from.
type LocationKind string

const (
	LocationStack     LocationKind = "stack"
	LocationMemory    LocationKind = "memory"
	LocationStorage   LocationKind = "storage"
	LocationTransient LocationKind = "transient"
	LocationCalldata  LocationKind = "calldata"
	LocationCode      LocationKind = "code"
	LocationNone      LocationKind = "none"
)

// Location describes where a value was sourced from.
type Location struct {
	Kind LocationKind `json:"kind"`
	// Slot is set for storage/transient (hex string of the 32-byte slot).
	Slot string `json:"slot,omitempty"`
	// Offset is set for memory/calldata/code (byte offset).
	Offset uint64 `json:"offset,omitempty"`
	// Length is the meaningful number of bytes at Offset (memory/code).
	Length uint64 `json:"length,omitempty"`
	// StackIndex is 1-based depth from top of stack (0 = unknown).
	StackIndex int `json:"stackIndex,omitempty"`
}

// Variable is the unit of varpeeker output: one named declaration with
// optional decoded value and provenance.
type Variable struct {
	Name           string       `json:"name"`
	Kind           VariableKind `json:"kind"`
	Type           string       `json:"type"`
	StorageLoc     string       `json:"storageLocation,omitempty"`
	DeclaredAtLine int          `json:"declaredAtLine,omitempty"`
	SourceID       int          `json:"sourceId,omitempty"`
	Value          string       `json:"value,omitempty"`
	Confidence     Confidence   `json:"confidence"`
	Location       Location     `json:"location,omitempty"`
	Note           string       `json:"note,omitempty"`
	// Children populates structured types (struct fields, dynamic-array
	// elements). For mappings Children is left empty: callers must
	// supply explicit keys via PeekStorageKey.
	Children []Variable `json:"children,omitempty"`
}

// Snapshot is the full peek result for a given execution point.
type Snapshot struct {
	Function   string     `json:"function,omitempty"`
	Contract   string     `json:"contract,omitempty"`
	PC         uint64     `json:"pc"`
	Locals     []Variable `json:"locals,omitempty"`
	Storage    []Variable `json:"storage,omitempty"`
	Transient  []Variable `json:"transient,omitempty"`
	Immutables []Variable `json:"immutables,omitempty"`
	Notes      []string   `json:"notes,omitempty"`
}

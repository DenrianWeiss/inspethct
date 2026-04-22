package oneshot

import (
	"math/big"

	"inspethct/internal/contractmeta"
	"inspethct/internal/engine"
	"inspethct/internal/forkengine"
	"inspethct/internal/forkengine/upstream"
)

const defaultExplorerAPIBase = "https://api.etherscan.io"

type projectKind string

const (
	projectUnknown projectKind = "unknown"
	projectFoundry projectKind = "foundry"
	projectHardhat projectKind = "hardhat"
	projectMixed   projectKind = "mixed"
)

type projectDetection struct {
	Root string
	Kind projectKind
}

type oneshotExecutionMode int

const (
	oneshoModeContinue oneshotExecutionMode = iota
	oneshoModeNext
)

type breakpointAccessMode string

const (
	accessModeRead  breakpointAccessMode = "read"
	accessModeWrite breakpointAccessMode = "write"
	accessModeBoth  breakpointAccessMode = "rw"
)

type oneshotBreakpointKind string

const (
	breakpointKindSource   oneshotBreakpointKind = "source"
	breakpointKindFunction oneshotBreakpointKind = "function"
	breakpointKindCall     oneshotBreakpointKind = "call"
	breakpointKindStorage  oneshotBreakpointKind = "storage"
	breakpointKindMemory   oneshotBreakpointKind = "memory"
)

type oneshotConfig struct {
	ProjectRoot         string
	UpstreamURL         string
	ExplorerAPIBase     string
	ExplorerAPIKey      string
	ExplorerChainID     string
	BlockRef            upstream.BlockRef
	Fork                engine.Fork
	Mode                forkengine.Mode
	ChainIDOverride     *big.Int
	ChainIDOverrideText string
}

type oneshotSession struct {
	config       oneshotConfig
	engineRef    *forkengine.Engine
	kind         string
	txHash       engine.Hash
	callReq      forkengine.CallRequest
	bundle       *contractmeta.Bundle
	breakpoints  []oneshotBreakpoint
	mutations    []oneshotMutation
	position     int
	lastStep     int
	done         bool
	result       *engine.ExecutionResult
	current      *oneshotPause
	pendingPause *oneshotPause
	targetAddr   *engine.Address
	codeAddr     *engine.Address
	rootInput    []byte
	exact        bool
	limitation   string
}

type oneshotBreakpoint struct {
	ID             int
	Kind           oneshotBreakpointKind
	Display        string
	SourceName     string
	Line           int
	Column         int
	PCs            []uint64
	Signature      string
	Selector       []byte
	AddressFilter  *engine.Address
	Slot           *engine.Hash
	SlotLabel      string
	Offset         uint64
	Size           uint64
	AccessMode     breakpointAccessMode
	Scope          string
	PartialMessage string
}

type oneshotMutation struct {
	Kind      string
	StepIndex int
	Offset    uint64
	Data      []byte
}

type oneshotPause struct {
	Reason          string
	StepIndex       int
	Breakpoint      string
	ContractAddress string
	CodeAddress     string
	MemorySize      int
	Source          map[string]any
	Storage         []oneshotVariableValue
	Transient       []oneshotVariableValue
	MemorySnapshot  []byte
	MemoryAccess    *oneshotMemoryAccess
	StorageAccess   *oneshotStorageAccess
	CallAccess      *oneshotCallAccess
	Step            oneshotStep
}

type oneshotMemoryAccess struct {
	Offset  uint64
	Size    uint64
	IsWrite bool
	Data    string
}

type oneshotStorageAccess struct {
	Slot    string
	Scope   string
	IsWrite bool
	Value   string
}

type oneshotCallAccess struct {
	Kind     string
	Caller   string
	Callee   string
	CodeAddr string
	Input    string
	Value    string
	Gas      uint64
}

type oneshotVariableValue struct {
	Scope string
	Name  string
	Slot  string
	Value string
	Type  string
}

type oneshotStep struct {
	PC           uint64
	Op           string
	Depth        int
	GasRemaining uint64
	GasCost      uint64
}

type oneshotStepHook struct {
	session *oneshotSession
	mode    oneshotExecutionMode
	seen    int
}

type oneshotAccessHook struct {
	session *oneshotSession
	kind    engine.HookType
	hookID  string
	reason  string
	matcher func(*engine.HookContext, oneshotBreakpoint) bool
	builder func(*engine.HookContext) any
}

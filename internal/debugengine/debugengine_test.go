package debugengine

import (
	"math/big"
	"testing"

	"inspethct/internal/engine"
	"inspethct/internal/srcmap"
)

func TestNewAnnotationHooksBridgeRuntimeAccess(t *testing.T) {
	input := []byte(`{
		"sources": {
			"A.sol": {
				"id": 0,
				"content": "contract C { uint256 x; function f() external { x = 1; } }",
				"ast": {
					"id": 1,
					"nodeType": "SourceUnit",
					"src": "0:58:0",
					"nodes": [
						{
							"id": 2,
							"nodeType": "ContractDefinition",
							"name": "C",
							"src": "0:58:0",
							"nodes": [
								{
									"id": 3,
									"nodeType": "VariableDeclaration",
									"name": "x",
									"src": "13:9:0"
								},
								{
									"id": 4,
									"nodeType": "FunctionDefinition",
									"name": "f",
									"src": "24:31:0"
								}
							]
						}
					]
				}
			}
		},
		"contracts": {
			"A.sol": {
				"C": {
					"storageLayout": {
						"storage": [
							{
								"astId": 3,
								"contract": "A.sol:C",
								"label": "x",
								"offset": 0,
								"slot": "0",
								"type": "t_uint256"
							}
						],
						"types": {
							"t_uint256": {
								"encoding": "inplace",
								"label": "uint256",
								"numberOfBytes": "32"
							}
						}
					},
					"transientStorageLayout": {"storage": [], "types": {}},
					"evm": {
						"bytecode": {
							"object": "6001604052600160005500",
							"sourceMap": "24:31:0;24:31:0;24:31:0;24:31:0;24:31:0;24:31:0;;",
							"generatedSources": []
						},
						"deployedBytecode": {
							"object": "6001604052600160005500",
							"sourceMap": "24:31:0;24:31:0;24:31:0;24:31:0;24:31:0;24:31:0;;",
							"generatedSources": []
						}
					}
				}
			}
		}
	}`)

	idx, err := srcmap.BuildIndexFromStandardJSON(input, srcmap.BuildConfig{SourceName: "A.sol", ContractName: "C", Runtime: true})
	if err != nil {
		t.Fatalf("BuildIndexFromStandardJSON() error = %v", err)
	}

	addr := engine.Address{19: 0x44}
	sto := engine.NewInMemoryStorage()
	acc := engine.NewInMemoryAccountState()
	code := idx.RuntimeBytecode
	state := engine.NewEVMState(
		engine.NewStack(),
		engine.NewMemory(),
		sto,
		engine.NewInMemoryTransientStorage(),
		acc,
		engine.NewGasMeter(100000),
		&engine.SimpleBlockContext{NumberVal: big.NewInt(1), ChainIDVal: big.NewInt(1)},
		&engine.SimpleTxContext{OriginVal: addr, GasPriceVal: big.NewInt(0)},
		&engine.SimpleContract{AddressVal: addr, CallerVal: addr, CallValueVal: big.NewInt(0), CodeVal: code, CodeHashVal: engine.Hash{}, CodeAddrVal: addr},
		engine.NewSimpleAccessList(),
	)

	var memorySeen []srcmap.MemoryAnnotation
	var storageSeen []srcmap.StorageAnnotation
	evm, err := NewEVM(state, engine.ForkLondon, idx, ObserverFuncs{
		Memory:  func(annotation srcmap.MemoryAnnotation) { memorySeen = append(memorySeen, annotation) },
		Storage: func(annotation srcmap.StorageAnnotation) { storageSeen = append(storageSeen, annotation) },
	}, nil)
	if err != nil {
		t.Fatalf("NewEVM() error = %v", err)
	}
	if _, err := evm.Run(code); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(memorySeen) == 0 {
		t.Fatalf("expected memory annotations")
	}
	if memorySeen[0].Region.Kind != srcmap.MemoryRegionFreePtr {
		t.Fatalf("memorySeen[0].Region.Kind = %s, want %s", memorySeen[0].Region.Kind, srcmap.MemoryRegionFreePtr)
	}
	if len(storageSeen) == 0 {
		t.Fatalf("expected storage annotations")
	}
	if len(storageSeen[0].Variables) != 1 || storageSeen[0].Variables[0].Entry.Label != "x" {
		t.Fatalf("unexpected storage annotation variables: %+v", storageSeen[0].Variables)
	}
}

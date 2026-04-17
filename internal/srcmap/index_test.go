package srcmap

import "testing"

func TestBuildIndexFromStandardJSON(t *testing.T) {
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
							"object": "600160005500",
							"sourceMap": "24:31:0;24:31:0;24:31:0;;",
							"generatedSources": []
						},
						"deployedBytecode": {
							"object": "600160005500",
							"sourceMap": "24:31:0;24:31:0;24:31:0;;",
							"generatedSources": []
						}
					}
				}
			}
		}
	}`)

	idx, err := BuildIndexFromStandardJSON(input, BuildConfig{SourceName: "A.sol", ContractName: "C", Runtime: true})
	if err != nil {
		t.Fatalf("BuildIndexFromStandardJSON() error = %v", err)
	}
	if len(idx.Instructions) != 4 {
		t.Fatalf("len(idx.Instructions) = %d, want 4", len(idx.Instructions))
	}
	if _, ok := idx.InstructionAtPC(2); !ok {
		t.Fatalf("InstructionAtPC(2) not found")
	}
	storage := idx.StorageByASTID(3)
	if len(storage) != 1 {
		t.Fatalf("len(StorageByASTID(3)) = %d, want 1", len(storage))
	}
	if storage[0].Entry.Label != "x" {
		t.Fatalf("storage label = %q, want x", storage[0].Entry.Label)
	}
	nodes := idx.NodesForSource(0, 24, 55)
	if len(nodes) == 0 {
		t.Fatalf("NodesForSource returned no nodes")
	}
	annotation := NewRuntimeBridge(idx).AnnotateMemoryAccess(MemoryAccess{PC: 0, Opcode: 0x52, Offset: 0x40, Size: 32, IsWrite: true})
	if annotation.Region.Kind != MemoryRegionFreePtr {
		t.Fatalf("memory region = %s, want %s", annotation.Region.Kind, MemoryRegionFreePtr)
	}
}

func TestStorageBySlotMatchesHexRuntimeSlot(t *testing.T) {
	idx := &Index{
		StorageVariables: []StorageVariableMapping{{
			Entry: StorageEntry{Label: "x", Slot: "0"},
			Scope: StorageScopePersistent,
		}},
	}
	matched := idx.StorageBySlot("0x0000000000000000000000000000000000000000000000000000000000000000", StorageScopePersistent)
	if len(matched) != 1 {
		t.Fatalf("len(matched) = %d, want 1", len(matched))
	}
	if matched[0].Entry.Label != "x" {
		t.Fatalf("matched label = %q, want x", matched[0].Entry.Label)
	}
}

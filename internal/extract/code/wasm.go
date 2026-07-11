// wasm.go hosts the embedded tree-sitter WASI module via wazero — pure Go,
// CGO_ENABLED=0, single binary. The module is compiled once; each worker
// goroutine gets its own instance (instances are not concurrent-safe, and
// separate instances give parallel parsing on separate memories).
package code

import (
	"context"
	_ "embed"
	"encoding/binary"
	"fmt"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

//go:embed treesitter.wasm
var wasmModule []byte

// RawNode is one preorder record from the shim (ABI: 6 little-endian u32).
type RawNode struct {
	Symbol   uint32
	Start    uint32
	End      uint32
	StartRow uint32
	EndRow   uint32
	Depth    uint32
	Named    bool
}

const recordSize = 24

type Engine struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
	names    atomic.Uint64 // instance name counter
}

func NewEngine(ctx context.Context) (*Engine, error) {
	return NewEngineFromBytes(ctx, wasmModule)
}

// NewEngineFromBytes hosts any module built against the shim ABI — the
// embedded bundle or a single-grammar pack.
func NewEngineFromBytes(ctx context.Context, module []byte) (*Engine, error) {
	rt := wazero.NewRuntime(ctx)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	compiled, err := rt.CompileModule(ctx, module)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("compile wasm module: %w", err)
	}
	return &Engine{rt: rt, compiled: compiled}, nil
}

func (e *Engine) Close(ctx context.Context) { e.rt.Close(ctx) }

// Instance is one guest with its own memory. NOT safe for concurrent use —
// one per worker.
type Instance struct {
	mod                                         api.Module
	alloc, free, parse, symbols, outPtr, outLen api.Function
}

func (e *Engine) NewInstance(ctx context.Context) (*Instance, error) {
	cfg := wazero.NewModuleConfig().WithName(fmt.Sprintf("ts-%d", e.names.Add(1)))
	mod, err := e.rt.InstantiateModule(ctx, e.compiled, cfg)
	if err != nil {
		return nil, fmt.Errorf("instantiate: %w", err)
	}
	i := &Instance{
		mod:     mod,
		alloc:   mod.ExportedFunction("co_alloc"),
		free:    mod.ExportedFunction("co_free"),
		parse:   mod.ExportedFunction("co_parse"),
		symbols: mod.ExportedFunction("co_symbols"),
		outPtr:  mod.ExportedFunction("co_out_ptr"),
		outLen:  mod.ExportedFunction("co_out_len"),
	}
	for name, f := range map[string]api.Function{"co_alloc": i.alloc, "co_free": i.free,
		"co_parse": i.parse, "co_symbols": i.symbols, "co_out_ptr": i.outPtr, "co_out_len": i.outLen} {
		if f == nil {
			return nil, fmt.Errorf("wasm export %s missing", name)
		}
	}
	return i, nil
}

func (i *Instance) Close(ctx context.Context) { i.mod.Close(ctx) }

func (i *Instance) out(ctx context.Context) ([]byte, error) {
	p, err := i.outPtr.Call(ctx)
	if err != nil {
		return nil, err
	}
	l, err := i.outLen.Call(ctx)
	if err != nil {
		return nil, err
	}
	buf, ok := i.mod.Memory().Read(uint32(p[0]), uint32(l[0]))
	if !ok {
		return nil, fmt.Errorf("wasm out buffer out of range")
	}
	return buf, nil
}

// Symbols returns the language's symbol-name table (index = symbol id).
func (i *Instance) Symbols(ctx context.Context, langID int) ([]string, error) {
	rc, err := i.symbols.Call(ctx, uint64(uint32(langID)))
	if err != nil {
		return nil, err
	}
	if int32(rc[0]) != 0 {
		return nil, fmt.Errorf("co_symbols(%d) rc=%d", langID, int32(rc[0]))
	}
	buf, err := i.out(ctx)
	if err != nil {
		return nil, err
	}
	if len(buf) < 4 {
		return nil, fmt.Errorf("short symbols buffer")
	}
	n := binary.LittleEndian.Uint32(buf)
	names := make([]string, 0, n)
	rest := buf[4:]
	for len(names) < int(n) {
		var j int
		for j = 0; j < len(rest) && rest[j] != 0; j++ {
		}
		if j == len(rest) {
			return nil, fmt.Errorf("truncated symbol table")
		}
		names = append(names, string(rest[:j]))
		rest = rest[j+1:]
	}
	return names, nil
}

// Parse runs the grammar over src and returns the preorder dump.
func (i *Instance) Parse(ctx context.Context, langID int, src []byte) ([]RawNode, error) {
	if len(src) == 0 {
		return nil, nil
	}
	p, err := i.alloc.Call(ctx, uint64(len(src)))
	if err != nil {
		return nil, err
	}
	ptr := uint32(p[0])
	if ptr == 0 {
		return nil, fmt.Errorf("guest alloc failed")
	}
	defer i.free.Call(ctx, uint64(ptr))
	if !i.mod.Memory().Write(ptr, src) {
		return nil, fmt.Errorf("guest write out of range")
	}
	rc, err := i.parse.Call(ctx, uint64(uint32(langID)), uint64(ptr), uint64(uint32(len(src))))
	if err != nil {
		return nil, err
	}
	if int32(rc[0]) != 0 {
		return nil, fmt.Errorf("co_parse rc=%d", int32(rc[0]))
	}
	buf, err := i.out(ctx)
	if err != nil {
		return nil, err
	}
	if len(buf) < 4 {
		return nil, fmt.Errorf("short parse buffer")
	}
	count := binary.LittleEndian.Uint32(buf)
	if uint64(len(buf)) < 4+uint64(count)*recordSize {
		return nil, fmt.Errorf("truncated parse buffer: %d records, %d bytes", count, len(buf))
	}
	nodes := make([]RawNode, count)
	off := 4
	for k := range nodes {
		df := binary.LittleEndian.Uint32(buf[off+20:])
		nodes[k] = RawNode{
			Symbol:   binary.LittleEndian.Uint32(buf[off:]),
			Start:    binary.LittleEndian.Uint32(buf[off+4:]),
			End:      binary.LittleEndian.Uint32(buf[off+8:]),
			StartRow: binary.LittleEndian.Uint32(buf[off+12:]),
			EndRow:   binary.LittleEndian.Uint32(buf[off+16:]),
			Depth:    df >> 1,
			Named:    df&1 == 1,
		}
		off += recordSize
	}
	return nodes, nil
}

//go:build wasip1

package pluginsdk

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"
)

// 宿主调用：响应缓冲区由插件预先分配，宿主直接写入，避免在宿主调用期间回调插件。
//
//go:wasmimport host call
//go:noescape
func hostCall(namePtr, nameLen, reqPtr, reqLen, respPtr, respCap uint32) int64

var (
	pinnedMu sync.Mutex
	pinned   = map[uint32][]byte{}
)

//go:wasmexport alloc
func alloc(size uint32) uint32 {
	if size == 0 {
		size = 1
	}
	b := make([]byte, size)
	ptr := uint32(uintptr(unsafe.Pointer(&b[0])))
	pinnedMu.Lock()
	pinned[ptr] = b
	pinnedMu.Unlock()
	return ptr
}

//go:wasmexport dealloc
func dealloc(ptr, size uint32) {
	pinnedMu.Lock()
	delete(pinned, ptr)
	pinnedMu.Unlock()
}

//go:wasmexport handle
func handle(ptr, size uint32) uint64 {
	input := readMemory(ptr, size)
	out := Handle(input)
	outPtr := alloc(uint32(len(out)))
	copy(readMemory(outPtr, uint32(len(out))), out)
	return uint64(outPtr) | uint64(len(out))<<32
}

func init() {
	hostCallFn = wasmHostCall
}

func readMemory(ptr, size uint32) []byte {
	if size == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), size)
}

func wasmHostCall(name string, req, out any) error {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return err
	}
	nameBytes := []byte(name)

	respCap := uint32(64 * 1024)
	// 第一次调用执行动作本身；若缓冲区不足，第二次改用 host.take 取回已暂存的
	// 结果，不重新执行动作，避免 HTTP 等带副作用的调用被执行两次。
	callName := nameBytes
	for range 2 {
		namePtr := alloc(uint32(len(callName)))
		copy(readMemory(namePtr, uint32(len(callName))), callName)
		reqPtr := alloc(uint32(len(reqBytes)))
		copy(readMemory(reqPtr, uint32(len(reqBytes))), reqBytes)
		respPtr := alloc(respCap)

		n := hostCall(namePtr, uint32(len(callName)), reqPtr, uint32(len(reqBytes)), respPtr, respCap)

		dealloc(namePtr, uint32(len(callName)))
		dealloc(reqPtr, uint32(len(reqBytes)))

		if n == -2 {
			need := binary.LittleEndian.Uint64(readMemory(respPtr, 8))
			dealloc(respPtr, respCap)
			if need == 0 || need > 1<<28 {
				return fmt.Errorf("invalid host response size %d", need)
			}
			respCap = uint32(need)
			callName = []byte("host.take")
			continue
		}
		if n < 0 {
			dealloc(respPtr, respCap)
			return fmt.Errorf("host call %q failed", name)
		}
		raw := append([]byte(nil), readMemory(respPtr, uint32(n))...)
		dealloc(respPtr, respCap)

		var resp Response
		if err := json.Unmarshal(raw, &resp); err != nil {
			return err
		}
		if !resp.OK {
			if resp.Error == nil {
				return fmt.Errorf("host call %q failed", name)
			}
			return resp.Error
		}
		if out != nil && len(resp.Data) != 0 {
			return json.Unmarshal(resp.Data, out)
		}
		return nil
	}
	return fmt.Errorf("host call %q response too large", name)
}

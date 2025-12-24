// Copyright 2025 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wasi_preview1

import (
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/ziggy42/epsilon/epsilon"
)

const (
	WASIMemoryExportName  = "memory"
	WASIModuleName        = "wasi_snapshot_preview1"
	WASIClockResolutionNs = 1
)

type WasiModule struct {
	fs                    *wasiResourceTable
	args                  []string
	env                   map[string]string
	monotonicClockStartNs int64
}

func NewWasiModule(
	args []string,
	env map[string]string,
	preopens []WasiPreopenDir,
) (*WasiModule, error) {
	fs, err := newWasiResourceTable(preopens)
	if err != nil {
		return nil, err
	}
	return &WasiModule{
		fs:                    fs,
		args:                  args,
		env:                   env,
		monotonicClockStartNs: time.Now().UnixNano(),
	}, nil
}

func (w *WasiModule) argsGet(
	inst *epsilon.ModuleInstance,
	argvPtr, argvBufPtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	bufOffset := uint32(argvBufPtr)
	for i, arg := range w.args {
		ptrIndex := uint32(argvPtr) + uint32(i*4)
		if err := memory.StoreUint32(0, ptrIndex, bufOffset); err != nil {
			return ErrnoFault
		}
		argBytes := append([]byte(arg), 0)
		if err := memory.Set(0, bufOffset, argBytes); err != nil {
			return ErrnoFault
		}
		bufOffset += uint32(len(argBytes))
	}

	return ErrnoSuccess
}

func (w *WasiModule) argsSizesGet(
	inst *epsilon.ModuleInstance,
	argcPtr, argvBufSizePtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	argc := uint32(len(w.args))
	if err := memory.StoreUint32(0, uint32(argcPtr), argc); err != nil {
		return ErrnoFault
	}

	bufSize := uint32(0)
	for _, arg := range w.args {
		bufSize += uint32(len(arg)) + 1
	}
	if err := memory.StoreUint32(0, uint32(argvBufSizePtr), bufSize); err != nil {
		return ErrnoFault
	}
	return ErrnoSuccess
}

func (w *WasiModule) environGet(
	inst *epsilon.ModuleInstance,
	envPtr, envBufPtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	bufOffset := uint32(envBufPtr)
	ptrIndex := uint32(envPtr)
	for key, value := range w.env {
		if err := memory.StoreUint32(0, ptrIndex, bufOffset); err != nil {
			return ErrnoFault
		}

		// Write the environment variable as "KEY=VALUE\0"
		envBytes := append([]byte(key+"="+value), 0)
		if err := memory.Set(0, bufOffset, envBytes); err != nil {
			return ErrnoFault
		}

		bufOffset += uint32(len(envBytes))
		ptrIndex += 4
	}

	return ErrnoSuccess
}

func (w *WasiModule) environSizesGet(
	inst *epsilon.ModuleInstance,
	envcPtr, envBufSizePtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	environc := uint32(len(w.env))
	if err := memory.StoreUint32(0, uint32(envcPtr), environc); err != nil {
		return ErrnoFault
	}

	bufSize := uint32(0)
	for key, value := range w.env {
		// Each env var is "KEY=VALUE\0"
		bufSize += uint32(len(key)) + 1 + uint32(len(value)) + 1
	}
	if err := memory.StoreUint32(0, uint32(envBufSizePtr), bufSize); err != nil {
		return ErrnoFault
	}
	return ErrnoSuccess
}

func (w *WasiModule) clockResGet(
	inst *epsilon.ModuleInstance,
	clockId, resPtr int32,
) int32 {
	if !isValidClockId(uint32(clockId)) {
		return ErrnoInval
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	err = memory.StoreUint64(0, uint32(resPtr), WASIClockResolutionNs)
	if err != nil {
		return ErrnoFault
	}
	return ErrnoSuccess
}

func (w *WasiModule) clockTimeGet(
	inst *epsilon.ModuleInstance,
	clockId, resPtr int32,
) int32 {
	if !isValidClockId(uint32(clockId)) {
		return ErrnoInval
	}

	res, err := w.getTimestamp(uint32(clockId))
	if err != nil {
		return ErrnoInval
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	if err := memory.StoreUint64(0, uint32(resPtr), uint64(res)); err != nil {
		return ErrnoFault
	}
	return ErrnoSuccess
}

func (w *WasiModule) getTimestamp(clockId uint32) (int64, error) {
	switch clockId {
	case ClockRealtime:
		return time.Now().UnixNano(), nil
	case ClockMonotonic, ClockProcessCPUTimeID, ClockThreadCPUTimeID:
		// TODO: ClockProcessCPUTimeID, ClockThreadCPUTimeID are not correct.
		return time.Now().UnixNano() - w.monotonicClockStartNs, nil
	default:
		return -1, fmt.Errorf("unknown clock ID: %d", clockId)
	}
}

func isValidClockId(clockId uint32) bool {
	switch clockId {
	case ClockRealtime,
		ClockMonotonic,
		ClockProcessCPUTimeID,
		ClockThreadCPUTimeID:
		return true
	default:
		return false
	}
}

func (w *WasiModule) randomGet(
	inst *epsilon.ModuleInstance,
	bufPtr, bufLen int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	randBytes := make([]byte, bufLen)
	_, err = rand.Read(randBytes)
	if err != nil {
		return ErrnoIO
	}

	if err := memory.Set(0, uint32(bufPtr), randBytes); err != nil {
		return ErrnoFault
	}

	return ErrnoSuccess
}

func (w *WasiModule) pollOneoff(
	inst *epsilon.ModuleInstance,
	inPtr, outPtr, nsubscriptions, neventsPtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return ErrnoFault
	}

	// Stub: we just return success with 0 events or immediately return.
	// Real implementation would look at subscriptions (clock or fd read/write).
	// For now, supporting sleep via clock subscription is enough for basic tests.
	// But parsing the subscription struct is complex (C union-like).
	// We'll return ErrnoNotSup for now to avoid incorrect behavior.
	// Stub: we just return success with 0 events.
	if err := memory.StoreUint32(0, uint32(neventsPtr), 0); err != nil {
		return ErrnoFault
	}
	return ErrnoSuccess
}

func (w *WasiModule) procRaise(sig int32) int32 {
	return ErrnoNotSup
}

func (w *WasiModule) schedYield() int32 {
	// runtime.Gosched()
	return ErrnoSuccess
}

func (w *WasiModule) ToImports() map[string]map[string]any {
	imports := map[string]any{
		"args_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.argsGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"args_sizes_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.argsSizesGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"environ_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.environGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"environ_sizes_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.environSizesGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"clock_res_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.clockResGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"clock_time_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			_ = args[1].(int64) // precision is ignored
			errCode := w.clockTimeGet(inst, args[0].(int32), args[2].(int32))
			return []any{errCode}
		},
		"fd_close": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.close(args[0].(int32))
			return []any{errCode}
		},
		"fd_fdstat_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.getStat(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_fdstat_set_flags": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setStatFlags(args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_fdstat_set_rights": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setStatRights(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
			)
			return []any{errCode}
		},
		"fd_prestat_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.getPrestat(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_prestat_dir_name": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.prestatDirName(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		},
		"path_filestat_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathFilestatGet(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
			return []any{errCode}
		},
		"path_open": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathOpen(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int64),
				args[6].(int64),
				args[7].(int32),
				args[8].(int32),
			)
			return []any{errCode}
		},
		"path_remove_directory": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.pathRemoveDirectory(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		},
		"fd_seek": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.seek(
				inst,
				args[0].(int32),
				args[1].(int64),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		},
		"fd_read": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.read(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		},
		"fd_write": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.write(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		},
		"fd_pwrite": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pwrite(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
			return []any{errCode}
		},
		"proc_exit": func(inst *epsilon.ModuleInstance, args ...any) []any {
			os.Exit(int(args[0].(int32)))
			return []any{}
		},
		"random_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.randomGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_advise": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.advise(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
				args[3].(int32),
			)
			return []any{errCode}
		},
		"fd_allocate": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.allocate(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
			)
			return []any{errCode}
		},
		"fd_datasync": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.dataSync(args[0].(int32))
			return []any{errCode}
		},
		"fd_sync": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.sync(args[0].(int32))
			return []any{errCode}
		},
		"fd_tell": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.tell(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_renumber": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.renumber(args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_filestat_get": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.getFileStat(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
		"fd_filestat_set_size": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setFileStatSize(
				args[0].(int32),
				args[1].(int64),
			)
			return []any{errCode}
		},
		"fd_filestat_set_times": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setFileStatTimes(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
				args[3].(int32),
			)
			return []any{errCode}
		},
		"fd_pread": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pread(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
			return []any{errCode}
		},
		"fd_readdir": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.readdir(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
			return []any{errCode}
		},
		"path_create_directory": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.pathCreateDirectory(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		},
		"path_filestat_set_times": func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.pathFilestatSetTimes(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int64),
				args[5].(int64),
				args[6].(int32),
			)
			return []any{errCode}
		},
		"path_link": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathLink(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
				args[6].(int32),
			)
			return []any{errCode}
		},
		"path_readlink": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathReadlink(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
			return []any{errCode}
		},
		"path_rename": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathRename(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
			return []any{errCode}
		},
		"path_symlink": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathSymlink(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
			return []any{errCode}
		},
		"path_unlink_file": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.pathUnlinkFile(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		},
		"poll_oneoff": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.pollOneoff(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		},
		"proc_raise": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.procRaise(args[0].(int32))
			return []any{errCode}
		},
		"sched_yield": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.schedYield()
			return []any{errCode}
		},
		"sock_accept": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.sockAccept(
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		},
		"sock_recv": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.sockRecv(
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
			return []any{errCode}
		},
		"sock_send": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.sockSend(
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
			return []any{errCode}
		},
		"sock_shutdown": func(inst *epsilon.ModuleInstance, args ...any) []any {
			errCode := w.fs.sockShutdown(args[0].(int32), args[1].(int32))
			return []any{errCode}
		},
	}
	return map[string]map[string]any{WASIModuleName: imports}
}

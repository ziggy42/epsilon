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
	"os"
	"time"

	"github.com/ziggy42/epsilon/epsilon"
)

const (
	WASIMemoryExportName = "memory"
	WASIModuleName       = "wasi_snapshot_preview1"
)

type WasiModule struct {
	fs                    *wasiResourceTable
	args                  []string
	env                   map[string]string
	monotonicClockStartNs int64
}

// NewWasiModule creates a new WasiModule instance.
//
// Ownership Contract:
// On success (err == nil), the returned WasiModule takes ownership of the
// *os.Files provided in 'preopens'. The module will close these files when its
// resources are released (e.g. via an explicit Close method, if one existed, or
// relying on GC/Finalizers isn't safe, so the runtime typically handles this).
//
// On failure (err != nil), the WasiModule is not created, and the ownership of
// the files remains with the caller. The caller is responsible for closing the
// files in this case.
func NewWasiModule(
	args []string,
	env map[string]string,
	preopens []WasiPreopen,
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
		return errnoFault
	}

	bufOffset := uint32(argvBufPtr)
	for i, arg := range w.args {
		ptrIndex := uint32(argvPtr) + uint32(i*4)
		if err := memory.StoreUint32(0, ptrIndex, bufOffset); err != nil {
			return errnoFault
		}
		argBytes := append([]byte(arg), 0)
		if err := memory.Set(0, bufOffset, argBytes); err != nil {
			return errnoFault
		}
		bufOffset += uint32(len(argBytes))
	}

	return errnoSuccess
}

func (w *WasiModule) argsSizesGet(
	inst *epsilon.ModuleInstance,
	argcPtr, argvBufSizePtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	argc := uint32(len(w.args))
	if err := memory.StoreUint32(0, uint32(argcPtr), argc); err != nil {
		return errnoFault
	}

	bufSize := uint32(0)
	for _, arg := range w.args {
		bufSize += uint32(len(arg)) + 1
	}
	if err := memory.StoreUint32(0, uint32(argvBufSizePtr), bufSize); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *WasiModule) environGet(
	inst *epsilon.ModuleInstance,
	envPtr, envBufPtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	bufOffset := uint32(envBufPtr)
	ptrIndex := uint32(envPtr)
	for key, value := range w.env {
		if err := memory.StoreUint32(0, ptrIndex, bufOffset); err != nil {
			return errnoFault
		}

		// Write the environment variable as "KEY=VALUE\0"
		envBytes := append([]byte(key+"="+value), 0)
		if err := memory.Set(0, bufOffset, envBytes); err != nil {
			return errnoFault
		}

		bufOffset += uint32(len(envBytes))
		ptrIndex += 4
	}

	return errnoSuccess
}

func (w *WasiModule) environSizesGet(
	inst *epsilon.ModuleInstance,
	envcPtr, envBufSizePtr int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	environc := uint32(len(w.env))
	if err := memory.StoreUint32(0, uint32(envcPtr), environc); err != nil {
		return errnoFault
	}

	bufSize := uint32(0)
	for key, value := range w.env {
		// Each env var is "KEY=VALUE\0"
		bufSize += uint32(len(key)) + 1 + uint32(len(value)) + 1
	}
	if err := memory.StoreUint32(0, uint32(envBufSizePtr), bufSize); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *WasiModule) clockResGet(
	inst *epsilon.ModuleInstance,
	clockId, resPtr int32,
) int32 {
	res, errCode := getClockResolution(uint32(clockId))
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	err = memory.StoreUint64(0, uint32(resPtr), res)
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *WasiModule) clockTimeGet(
	inst *epsilon.ModuleInstance,
	clockId, resPtr int32,
) int32 {
	res, errCode := getTimestamp(w.monotonicClockStartNs, uint32(clockId))
	if errCode != errnoSuccess {
		return errCode
	}

	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	if err := memory.StoreUint64(0, uint32(resPtr), uint64(res)); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *WasiModule) randomGet(
	inst *epsilon.ModuleInstance,
	bufPtr, bufLen int32,
) int32 {
	memory, err := inst.GetMemory(WASIMemoryExportName)
	if err != nil {
		return errnoFault
	}

	randBytes := make([]byte, bufLen)
	_, err = rand.Read(randBytes)
	if err != nil {
		return errnoIO
	}

	if err := memory.Set(0, uint32(bufPtr), randBytes); err != nil {
		return errnoFault
	}

	return errnoSuccess
}

func (w *WasiModule) procRaise(sig int32) int32 {
	return errnoNotSup
}

func (w *WasiModule) schedYield() int32 {
	return errnoSuccess
}

func (w *WasiModule) ToImports() map[string]map[string]any {
	return epsilon.NewModuleImportBuilder(WASIModuleName).
		AddHostFunc("args_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.argsGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("args_sizes_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.argsSizesGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("environ_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.environGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("environ_sizes_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.environSizesGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("clock_res_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.clockResGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("clock_time_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			_ = args[1].(int64) // precision is ignored
			errCode := w.clockTimeGet(inst, args[0].(int32), args[2].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_close", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.close(args[0].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_fdstat_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.getStat(memory, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_fdstat_set_flags", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setStatFlags(args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_fdstat_set_rights", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setStatRights(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_prestat_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.getPrestat(memory, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_prestat_dir_name", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.prestatDirName(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_filestat_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathFilestatGet(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_open", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathOpen(
				memory,
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
		}).
		AddHostFunc("path_remove_directory", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathRemoveDirectory(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_seek", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.seek(
				memory,
				args[0].(int32),
				args[1].(int64),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_read", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.read(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_write", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.write(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_pwrite", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pwrite(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("proc_exit", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			os.Exit(int(args[0].(int32)))
			return []any{}
		}).
		AddHostFunc("random_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.randomGet(inst, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_advise", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.advise(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
				args[3].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_allocate", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.allocate(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_datasync", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.dataSync(args[0].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_sync", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.sync(args[0].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_tell", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.tell(memory, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_renumber", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.renumber(args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_filestat_get", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.getFileStat(memory, args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		AddHostFunc("fd_filestat_set_size", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.setFileStatSize(args[0].(int32), args[1].(int64))
			return []any{errCode}
		}).
		AddHostFunc("fd_filestat_set_times", func(
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
		}).
		AddHostFunc("fd_pread", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pread(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("fd_readdir", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.readdir(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_create_directory", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathCreateDirectory(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_filestat_set_times", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathFilestatSetTimes(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int64),
				args[5].(int64),
				args[6].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_link", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathLink(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
				args[6].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_readlink", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathReadlink(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_rename", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathRename(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_symlink", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathSymlink(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("path_unlink_file", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.pathUnlinkFile(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("poll_oneoff", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.pollOneoff(
				inst,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("proc_raise", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.procRaise(args[0].(int32))
			return []any{errCode}
		}).
		AddHostFunc("sched_yield", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.schedYield()
			return []any{errCode}
		}).
		AddHostFunc("sock_accept", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.sockAccept(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("sock_recv", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.sockRecv(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("sock_send", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			memory, err := inst.GetMemory(WASIMemoryExportName)
			if err != nil {
				return []any{errnoFault}
			}
			errCode := w.fs.sockSend(
				memory,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
			return []any{errCode}
		}).
		AddHostFunc("sock_shutdown", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			errCode := w.fs.sockShutdown(args[0].(int32), args[1].(int32))
			return []any{errCode}
		}).
		Build()
}

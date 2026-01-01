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

//go:build unix

package wasip1

import (
	"crypto/rand"
	"fmt"
	"os"
	"time"

	"github.com/ziggy42/epsilon/epsilon"
)

// WasiConfig contains configuration for creating a new WasiModule.
type WasiConfig struct {
	Args     []string
	Env      map[string]string
	Preopens []WasiPreopen
	Stdin    *os.File // If nil, os.Stdin is used.
	Stdout   *os.File // If nil, os.Stdout is used.
	Stderr   *os.File // If nil, os.Stderr is used.
}

type WasiModule struct {
	fs                    *wasiResourceTable
	args                  []string
	env                   map[string]string
	monotonicClockStartNs int64
}

// ProcExitError is an error that signals that the process should exit with the
// given code. It is used to implement proc_exit.
type ProcExitError struct {
	Code int32
}

func (e *ProcExitError) Error() string {
	return fmt.Sprintf("proc_exit: %d", e.Code)
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
func NewWasiModule(config WasiConfig) (*WasiModule, error) {
	stdin := config.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := config.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := config.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	fs, err := newWasiResourceTable(config.Preopens, stdin, stdout, stderr)
	if err != nil {
		return nil, err
	}
	return &WasiModule{
		fs:                    fs,
		args:                  config.Args,
		env:                   config.Env,
		monotonicClockStartNs: time.Now().UnixNano(),
	}, nil
}

func (w *WasiModule) argsGet(
	memory *epsilon.Memory,
	argvPtr, argvBufPtr int32,
) int32 {
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
	memory *epsilon.Memory,
	argcPtr, argvBufSizePtr int32,
) int32 {
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
	memory *epsilon.Memory,
	envPtr, envBufPtr int32,
) int32 {
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
	memory *epsilon.Memory,
	envcPtr, envBufSizePtr int32,
) int32 {
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
	memory *epsilon.Memory,
	clockId, resPtr int32,
) int32 {
	res, errCode := getClockResolution(uint32(clockId))
	if errCode != errnoSuccess {
		return errCode
	}

	err := memory.StoreUint64(0, uint32(resPtr), res)
	if err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *WasiModule) clockTimeGet(
	memory *epsilon.Memory,
	clockId, resPtr int32,
) int32 {
	res, errCode := getTimestamp(w.monotonicClockStartNs, uint32(clockId))
	if errCode != errnoSuccess {
		return errCode
	}

	if err := memory.StoreUint64(0, uint32(resPtr), uint64(res)); err != nil {
		return errnoFault
	}
	return errnoSuccess
}

func (w *WasiModule) randomGet(
	memory *epsilon.Memory,
	bufPtr, bufLen int32,
) int32 {
	randBytes := make([]byte, bufLen)
	_, err := rand.Read(randBytes)
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

func bind(fn func([]any) int32) func(*epsilon.ModuleInstance, ...any) []any {
	return func(_ *epsilon.ModuleInstance, args ...any) []any {
		return []any{fn(args)}
	}
}

func bindMem(
	fn func(*epsilon.Memory, []any) int32,
) func(*epsilon.ModuleInstance, ...any) []any {
	return func(inst *epsilon.ModuleInstance, args ...any) []any {
		memory, err := inst.GetMemory(WASIMemoryExportName)
		if err != nil {
			return []any{errnoFault}
		}
		return []any{fn(memory, args)}
	}
}

func (w *WasiModule) ToImports() map[string]map[string]any {
	return epsilon.NewModuleImportBuilder(WASIModuleName).
		AddHostFunc("args_get", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.argsGet(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("args_sizes_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.argsSizesGet(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("environ_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.environGet(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("environ_sizes_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.environSizesGet(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("clock_res_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.clockResGet(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("clock_time_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			_ = args[1].(int64) // precision is ignored
			return w.clockTimeGet(m, args[0].(int32), args[2].(int32))
		})).
		AddHostFunc("fd_close", bind(func(args []any) int32 {
			return w.fs.close(args[0].(int32))
		})).
		AddHostFunc("fd_fdstat_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.getStat(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_fdstat_set_flags", bind(func(args []any) int32 {
			return w.fs.setStatFlags(args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_fdstat_set_rights", bind(func(args []any) int32 {
			return w.fs.setStatRights(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
			)
		})).
		AddHostFunc("fd_prestat_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.getPrestat(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_prestat_dir_name", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.prestatDirName(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
		})).
		AddHostFunc("path_filestat_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathFilestatGet(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
		})).
		AddHostFunc("path_open", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.pathOpen(
				m,
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
		})).
		AddHostFunc("path_remove_directory", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathRemoveDirectory(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
		})).
		AddHostFunc("fd_seek", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.seek(
				m,
				args[0].(int32),
				args[1].(int64),
				args[2].(int32),
				args[3].(int32),
			)
		})).
		AddHostFunc("fd_read", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.read(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
		})).
		AddHostFunc("fd_write", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.write(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
			)
		})).
		AddHostFunc("fd_pwrite", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.pwrite(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
		})).
		AddHostFunc("proc_exit", func(
			inst *epsilon.ModuleInstance,
			args ...any,
		) []any {
			panic(&ProcExitError{Code: args[0].(int32)})
		}).
		AddHostFunc("random_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.randomGet(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_advise", bind(func(args []any) int32 {
			return w.fs.advise(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
				args[3].(int32),
			)
		})).
		AddHostFunc("fd_allocate", bind(func(args []any) int32 {
			return w.fs.allocate(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
			)
		})).
		AddHostFunc("fd_datasync", bind(func(args []any) int32 {
			return w.fs.dataSync(args[0].(int32))
		})).
		AddHostFunc("fd_sync", bind(func(args []any) int32 {
			return w.fs.sync(args[0].(int32))
		})).
		AddHostFunc("fd_tell", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.tell(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_renumber", bind(func(args []any) int32 {
			return w.fs.renumber(args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_filestat_get", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.getFileStat(m, args[0].(int32), args[1].(int32))
		})).
		AddHostFunc("fd_filestat_set_size", bind(func(args []any) int32 {
			return w.fs.setFileStatSize(args[0].(int32), args[1].(int64))
		})).
		AddHostFunc("fd_filestat_set_times", bind(func(args []any) int32 {
			return w.fs.setFileStatTimes(
				args[0].(int32),
				args[1].(int64),
				args[2].(int64),
				args[3].(int32),
			)
		})).
		AddHostFunc("fd_pread", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pread(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
		})).
		AddHostFunc("fd_readdir", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.readdir(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int64),
				args[4].(int32),
			)
		})).
		AddHostFunc("path_create_directory", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathCreateDirectory(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
		})).
		AddHostFunc("path_filestat_set_times", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathFilestatSetTimes(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int64),
				args[5].(int64),
				args[6].(int32),
			)
		})).
		AddHostFunc("path_link", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.pathLink(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
				args[6].(int32),
			)
		})).
		AddHostFunc("path_readlink", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathReadlink(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
		})).
		AddHostFunc("path_rename", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathRename(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
		})).
		AddHostFunc("path_symlink", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathSymlink(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
		})).
		AddHostFunc("path_unlink_file", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.pathUnlinkFile(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
		})).
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
		AddHostFunc("proc_raise", bind(func(args []any) int32 {
			return w.procRaise(args[0].(int32))
		})).
		AddHostFunc("sched_yield", bind(func(args []any) int32 {
			return w.schedYield()
		})).
		AddHostFunc("sock_accept", bindMem(func(
			m *epsilon.Memory,
			args []any,
		) int32 {
			return w.fs.sockAccept(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
			)
		})).
		AddHostFunc("sock_recv", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.sockRecv(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
				args[5].(int32),
			)
		})).
		AddHostFunc("sock_send", bindMem(func(m *epsilon.Memory, args []any) int32 {
			return w.fs.sockSend(
				m,
				args[0].(int32),
				args[1].(int32),
				args[2].(int32),
				args[3].(int32),
				args[4].(int32),
			)
		})).
		AddHostFunc("sock_shutdown", bind(func(args []any) int32 {
			return w.fs.sockShutdown(args[0].(int32), args[1].(int32))
		})).
		Build()
}

// Close releases all resources held by the WasiModule, including file
// descriptors for preopened directories and any files opened during execution.
// After calling Close, the WasiModule should not be used.
func (w *WasiModule) Close() {
	w.fs.closeAll()
}

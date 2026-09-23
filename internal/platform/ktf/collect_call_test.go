package ktf

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestCollectorKeepsNestedCallRegistersUntilUnwind(t *testing.T) {
	for _, worker := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			name := "service"
			if worker {
				name = "worker"
			}
			if fail {
				name += "/error"
			} else {
				name += "/return"
			}
			t.Run(name, func(t *testing.T) {
				client, runtime := newCollectorRuntime(t)
				parent := client.thread
				if worker {
					parent = armcore.NewThread(armcore.NewContext())
					client.workers = append(client.workers, &guestWorker{armThread: parent})
				}
				const code = 0x40000000
				words := []uint32{
					0xe1a04000, // mov r4, r0: sole reference in the outer call
					0xe3a00000, // mov r0, #0
					0xef000001, // svc #1: enter the inner call
					0xe12fff1e, // bx lr
					0xe1a05001, // mov r5, r1: sole reference in the inner call
					0xe3a01000, // mov r1, #0
					0xe3a04000, // mov r4, #0: leave the ancestor's reference behind
					0xef000002, // svc #2: collect while both calls are suspended
					0xe12fff1e, // bx lr
				}
				data := make([]byte, len(words)*4)
				for index, word := range words {
					binary.LittleEndian.PutUint32(data[index*4:], word)
				}
				if err := client.core.Memory().Map(code, uint64(len(data)), armcore.PermissionReadExecute); err != nil {
					t.Fatal(err)
				}
				if err := client.core.Memory().Load(code, data); err != nil {
					t.Fatal(err)
				}
				outer := allocateTestArray(t, runtime, 2)
				inner := allocateTestArray(t, runtime, 4)
				stopped := errors.New("stop nested call")
				_, err := client.core.Call(context.Background(), parent, code, code+0x100, []uint32{outer},
					func(ctx context.Context, thread *armcore.Thread, _ armcore.SupervisorCall) error {
						_, err := client.core.Call(ctx, thread, code+16, code+0x100, []uint32{0, inner},
							func(context.Context, *armcore.Thread, armcore.SupervisorCall) error {
								collectTwice(t, runtime)
								for _, address := range []uint32{outer, inner} {
									if _, ok := client.vm.AOTObject(address); !ok {
										t.Errorf("live nested register lost binding %#x", address)
									}
								}
								if fail {
									return stopped
								}
								return nil
							})
						return err
					})
				if (fail && !errors.Is(err, stopped)) || (!fail && err != nil) {
					t.Fatalf("nested call error = %v", err)
				}
				collectTwice(t, runtime)
				for _, address := range []uint32{outer, inner} {
					if _, tracked := runtime.objects[address]; tracked {
						t.Errorf("unwound call retained object %#x", address)
					}
				}
			})
		}
	}
}

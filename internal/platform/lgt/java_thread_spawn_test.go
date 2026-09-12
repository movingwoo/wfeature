package lgt

import (
	"context"
	"fmt"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestJavaThreadStartedDuringSliceSurvivesNextRound(t *testing.T) {
	for _, parentDone := range []bool{false, true} {
		t.Run(fmt.Sprintf("parent_done_%t", parentDone), func(t *testing.T) {
			client := fixtureClient(t)
			newWorker := func() *javaWorker {
				return &javaWorker{armThread: armcore.NewThread(armcore.NewContext()), grant: make(chan context.Context), events: make(chan javaWorkerEvent, 1)}
			}
			parent, child := newWorker(), newWorker()
			client.javaRuntimeState().workers = []*javaWorker{parent}
			go func() {
				<-parent.grant
				client.javaRun.workers = append(client.javaRun.workers, child)
				parent.events <- javaWorkerEvent{done: parentDone}
				if !parentDone {
					<-parent.grant
					parent.events <- javaWorkerEvent{done: true}
				}
			}()
			if n, err := client.ServiceJavaThreads(context.Background()); err != nil || n != 1 {
				t.Fatalf("parent round = %d, %v", n, err)
			}
			wantWorkers := 1
			if !parentDone {
				wantWorkers = 2
			}
			if len(client.javaRun.workers) != wantWorkers || client.javaRun.workers[wantWorkers-1] != child {
				t.Fatal("new child was dropped with the completed parent")
			}
			go func() { <-child.grant; child.events <- javaWorkerEvent{done: true} }()
			if n, err := client.ServiceJavaThreads(context.Background()); err != nil || n != wantWorkers {
				t.Fatalf("child round = %d, %v", n, err)
			}
			if len(client.javaRun.workers) != 0 {
				t.Fatal("completed child retained")
			}
		})
	}
}

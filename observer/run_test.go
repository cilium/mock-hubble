// Copyright 2021 Authors of Hubble
// Copyright 2021 Authors of Cilium
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

package observer_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/cilium/cilium/api/v1/flow"
	observer "github.com/cilium/cilium/api/v1/observer"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	. "github.com/cilium/mock-hubble/observer"
)

const (
	hubbleAddress = "localhost:4245"
)

func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fatal := make(chan error, 1)

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	go Run(ctx, log, "../testdata/2021-06-16-sample-flows-istio-gke", hubbleAddress, 100, nil, fatal)

	go func() {
		for err := range fatal {
			t.Errorf("fatal error in a goroutine: %v", err)
			cancel()
			return
		}
	}()

	hubbleConn, err := grpc.DialContext(ctx, hubbleAddress, grpc.WithInsecure())
	if err != nil {
		t.Fatalf("failed to connect to Hubble server: %v", err)
	}
	defer hubbleConn.Close()

	flowObsever, err := observer.NewObserverClient(hubbleConn).
		GetFlows(ctx, &observer.GetFlowsRequest{Follow: true})
	if err != nil {
		t.Fatalf("failed to create Hubble client: %v", err)
		return
	}

	fv := &flowValidator{
		today: time.Now().YearDay(),
	}

	for {
		hubbleResp, err := flowObsever.Recv()
		switch err {
		case io.EOF, context.Canceled:
			return
		case nil:
		default:
			if status.Code(err) == codes.Canceled {
				return
			}
			if !isEOF(err) {
				t.Fatalf("Hubble connection cancelled unexpectedly: %v", err)
			}
			if fv.counter != 20000 {
				t.Error("unexpected flow count")
			}
			return
		}

		fv.validateFlow(t, hubbleResp.GetFlow())
	}

}

type flowValidator struct {
	today   int
	counter int
}

func (fv *flowValidator) validateFlow(t *testing.T, f *flow.Flow) {
	t.Helper()

	fv.counter++

	// ensure that flow timestamps are adjusted to current time
	if f.Time.AsTime().YearDay() != fv.today {
		t.Error("flow timestamp has not been adjusted corretly")
	}

	// ensure that same flows are replayed to each new client
	if fv.counter == 1 &&
		f.Ethernet.Source != "46:e5:ea:1f:95:ff" &&
		f.Ethernet.Destination != "d6:ec:b0:4a:ff:46" {
		t.Error("unexpected first flow")
	}
}

func isEOF(err error) bool {
	s, ok := status.FromError(err)
	return ok && s.Proto().GetMessage() == "EOF"
}

// Copyright 2016 The LUCI Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prpc

import "google.golang.org/grpc"

// Registrar can register a service. It is implemented by *grpc.Server
// and used instead of grpc.Server in the code generated by cproto.
type Registrar interface {
	// RegisterService registers a service and its implementation.
	// Called from the generated code.
	RegisterService(desc *grpc.ServiceDesc, impl any)
}
